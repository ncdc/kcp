/*
Copyright 2022 The KCP Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package initialization

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/kcp-dev/logicalcluster/v2"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"

	apisv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apis/v1alpha1"
	"github.com/kcp-dev/kcp/pkg/apis/tenancy/initialization"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	conditionsv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/third_party/conditions/apis/conditions/v1alpha1"
	"github.com/kcp-dev/kcp/pkg/apis/third_party/conditions/util/conditions"
	"github.com/kcp-dev/kcp/pkg/logging"
)

const (
	// WorkspaceAPIBindingsInitialized represents the status of the initial APIBindings for the workspace.
	WorkspaceAPIBindingsInitialized conditionsv1alpha1.ConditionType = "APIBindingsInitialized"
	// WorkspaceInitializedWaitingOnAPIBindings is a reason for the APIBindingsInitialized condition that indicates
	// at least one APIBinding is not ready.
	WorkspaceInitializedWaitingOnAPIBindings = "WaitingOnAPIBindings"
	// WorkspaceInitializedClusterWorkspaceTypeInvalid is a reason for the APIBindingsInitialized
	// condition that indicates something is invalid with the ClusterWorkspaceType (e.g. a cycle trying
	// to resolve all the transitive types).
	WorkspaceInitializedClusterWorkspaceTypeInvalid = "ClusterWorkspaceTypeInvalid"
	// WorkspaceInitializedAPIBindingErrors is a reason for the APIBindingsInitialized condition that indicates there
	// were errors trying to initialize APIBindings for the workspace.
	WorkspaceInitializedAPIBindingErrors = "APIBindingErrors"
)

func (b *APIBinder) reconcile(ctx context.Context, clusterWorkspace *tenancyv1alpha1.ClusterWorkspace) error {
	logger := klog.FromContext(ctx).WithValues(
		"clusterWorkspaceType.path", clusterWorkspace.Spec.Type.Path,
		"clusterWorkspaceType.name", clusterWorkspace.Spec.Type.Name,
	)

	var errors []error
	clusterName := logicalcluster.From(clusterWorkspace).Join(clusterWorkspace.Name)
	logger.V(2).Info("initializing APIBindings for workspace")

	// Start with the ClusterWorkspaceType specified by the ClusterWorkspace
	leafCWT, err := b.getClusterWorkspaceType(logicalcluster.New(clusterWorkspace.Spec.Type.Path), string(clusterWorkspace.Spec.Type.Name))
	if err != nil {
		logger.Error(err, "error getting ClusterWorkspaceType")

		conditions.MarkFalse(
			clusterWorkspace,
			WorkspaceAPIBindingsInitialized,
			WorkspaceInitializedClusterWorkspaceTypeInvalid,
			conditionsv1alpha1.ConditionSeverityError,
			"error getting ClusterWorkspaceType %s|%s: %v",
			clusterWorkspace.Spec.Type.Path, clusterWorkspace.Spec.Type.Name,
			err,
		)

		return nil
	}

	// Get all the transitive ClusterWorkspaceTypes
	cwts, err := b.transitiveTypeResolver.Resolve(leafCWT)
	if err != nil {
		logger.Error(err, "error resolving transitive types")

		conditions.MarkFalse(
			clusterWorkspace,
			WorkspaceAPIBindingsInitialized,
			WorkspaceInitializedClusterWorkspaceTypeInvalid,
			conditionsv1alpha1.ConditionSeverityError,
			"error resolving transitive set of cluster workspace types: %v",
			err,
		)

		return nil
	}

	// Get current bindings
	bindings, err := b.listAPIBindings(clusterName)
	if err != nil {
		errors = append(errors, err)
	}

	// This keeps track of which APIBindings have been created for which APIExports
	exportToBinding := map[apisv1alpha1.WorkspaceExportReference]*apisv1alpha1.APIBinding{}

	for i := range bindings {
		binding := bindings[i]

		if binding.Spec.Reference.Workspace == nil {
			continue
		}

		// Track what we have ("actual")
		exportToBinding[*binding.Spec.Reference.Workspace] = binding
	}

	requiredExportRefs := map[tenancyv1alpha1.APIExportReference]struct{}{}

	for _, cwt := range cwts {
		logger := logging.WithObject(logger, cwt)
		logger.V(2).Info("attempting to initialize APIBindings")

		for i := range cwt.Spec.DefaultAPIBindings {
			exportRef := cwt.Spec.DefaultAPIBindings[i]

			// Keep track of unique set of expected exports across all CWTs
			requiredExportRefs[exportRef] = struct{}{}

			logger := logger.WithValues("apiExport.path", exportRef.Path, "apiExport.name", exportRef.Name)
			ctx := klog.NewContext(ctx, logger)

			// Make sure the APIExport exists
			apiExport, err := b.getAPIExport(logicalcluster.New(exportRef.Path), exportRef.Name)
			if err != nil {
				errors = append(errors, err)
				continue
			}

			apiBindingName := generateAPIBindingName(clusterName, exportRef.Path, exportRef.Name)
			logger = logger.WithValues("apiBindingName", apiBindingName)

			if _, err = b.getAPIBinding(clusterName, apiBindingName); err == nil {
				logger.V(4).Info("APIBinding already exists - skipping creation")
				continue
			}

			apiBinding := &apisv1alpha1.APIBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: apiBindingName,
				},
				Spec: apisv1alpha1.APIBindingSpec{
					Reference: apisv1alpha1.ExportReference{
						Workspace: &apisv1alpha1.WorkspaceExportReference{
							Path:       exportRef.Path,
							ExportName: exportRef.Name,
						},
					},
				},
			}

			for i := range apiExport.Spec.PermissionClaims {
				exportClaim := apiExport.Spec.PermissionClaims[i]

				acceptedClaim := apisv1alpha1.AcceptablePermissionClaim{
					PermissionClaim: exportClaim,
					State:           apisv1alpha1.ClaimAccepted,
				}

				apiBinding.Spec.PermissionClaims = append(apiBinding.Spec.PermissionClaims, acceptedClaim)
			}

			logger = logging.WithObject(logger, apiBinding)

			logger.V(2).Info("trying to create APIBinding")
			if _, err := b.createAPIBinding(ctx, clusterName, apiBinding); err != nil {
				if apierrors.IsAlreadyExists(err) {
					logger.V(2).Info("APIBinding already exists")
					continue
				}

				errors = append(errors, err)
				continue
			}

			logger.V(2).Info("created APIBinding")
		}
	}

	if len(errors) > 0 {
		logger.Error(utilerrors.NewAggregate(errors), "error initializing APIBindings")

		conditions.MarkFalse(
			clusterWorkspace,
			WorkspaceAPIBindingsInitialized,
			WorkspaceInitializedAPIBindingErrors,
			conditionsv1alpha1.ConditionSeverityError,
			"encountered errors: %v",
			utilerrors.NewAggregate(errors),
		)

		return nil
	}

	// Make sure all the expected bindings are there & ready to use
	var incomplete []string

	for exportRef := range requiredExportRefs {
		workspaceExportRef := apisv1alpha1.WorkspaceExportReference{
			Path:       exportRef.Path,
			ExportName: exportRef.Name,
		}

		binding, exists := exportToBinding[workspaceExportRef]
		if !exists {
			incomplete = append(incomplete, fmt.Sprintf("for APIExport %s|%s", exportRef.Path, exportRef.Name))
			continue
		}

		if !conditions.IsTrue(binding, apisv1alpha1.InitialBindingCompleted) {
			incomplete = append(incomplete, binding.Name)
		}
	}

	if len(incomplete) > 0 {
		sort.Strings(incomplete)

		conditions.MarkFalse(
			clusterWorkspace,
			WorkspaceAPIBindingsInitialized,
			WorkspaceInitializedWaitingOnAPIBindings,
			conditionsv1alpha1.ConditionSeverityInfo,
			"APIBinding(s) not yet fully bound: %s", strings.Join(incomplete, ", "),
		)

		return nil
	}

	clusterWorkspace.Status.Initializers = initialization.EnsureInitializerAbsent(tenancyv1alpha1.ClusterWorkspaceAPIBindingsInitializer, clusterWorkspace.Status.Initializers)

	return nil
}

// maxExportNamePrefixLength is the maximum allowed length for the export name portion of the generated API binding
// name. Subtrace 1 for the dash ("-") that separates the export name prefix from the hash suffix, and 5 for the
// hash length.
const maxExportNamePrefixLength = validation.DNS1123SubdomainMaxLength - 1 - 5

func generateAPIBindingName(clusterName logicalcluster.Name, exportPath, exportName string) string {
	maxLen := len(exportName)
	if maxLen > maxExportNamePrefixLength {
		maxLen = maxExportNamePrefixLength
	}

	exportNamePrefix := exportName[:maxLen]

	hash := toBase36Sha224(
		clusterName.String() + "|" + exportPath + "|" + exportName,
	)

	hash = hash[0:5]
	// Have to lowercase because Kubernetes names must be lowercase
	hash = strings.ToLower(hash)

	return fmt.Sprintf("%s-%s", exportNamePrefix, hash)
}

func toBase36(hash [28]byte) string {
	var i big.Int
	i.SetBytes(hash[:])
	return i.Text(62)
}

func toBase36Sha224(s string) string {
	return toBase36(sha256.Sum224([]byte(s)))
}
