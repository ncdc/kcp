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
	"fmt"
	"sort"
	"strings"

	"github.com/kcp-dev/logicalcluster/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	apisv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apis/v1alpha1"
	"github.com/kcp-dev/kcp/pkg/apis/tenancy/initialization"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	conditionsv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/third_party/conditions/apis/conditions/v1alpha1"
	"github.com/kcp-dev/kcp/pkg/apis/third_party/conditions/util/conditions"
	"github.com/kcp-dev/kcp/pkg/logging"
)

func (b *APIBinder) reconcile(ctx context.Context, clusterWorkspace *tenancyv1alpha1.ClusterWorkspace) error {
	logger := klog.FromContext(ctx)

	var errors []error
	clusterName := logicalcluster.From(clusterWorkspace).Join(clusterWorkspace.Name)
	logger.V(2).Info("initializing APIBindings for workspace")

	// Start with the ClusterWorkspaceType specified by the ClusterWorkspace
	leafCWT, err := b.getClusterWorkspaceType(logicalcluster.New(clusterWorkspace.Spec.Type.Path), string(clusterWorkspace.Spec.Type.Name))
	if err != nil {
		logger.Error(
			err,
			"error getting ClusterWorkspaceType",
			"clusterWorkspaceType.path", clusterWorkspace.Spec.Type.Path,
			"clusterWorkspaceType.name", clusterWorkspace.Spec.Type.Name,
		)

		conditions.MarkFalse(
			clusterWorkspace,
			tenancyv1alpha1.WorkspaceInitialized,
			tenancyv1alpha1.WorkspaceInitializedClusterWorkspaceTypeInvalid,
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
		logger.Error(
			err,
			"error resolving transitive types",
			"clusterWorkspaceType.path", clusterWorkspace.Spec.Type.Path,
			"clusterWorkspaceType.name", clusterWorkspace.Spec.Type.Name,
		)

		conditions.MarkFalse(
			clusterWorkspace,
			tenancyv1alpha1.WorkspaceInitialized,
			tenancyv1alpha1.WorkspaceInitializedClusterWorkspaceTypeInvalid,
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

	// This keeps track of any APIBindings for an APIExport that may have conflicts. We don't really expect this to
	// happen, because we're tracking what we've created, but "just in case"...
	bindingsWithConflicts := make(map[tenancyv1alpha1.APIExportReference]sets.String)

	// This keeps track of which APIBindings have been created for which APIExports
	exportToBinding := map[apisv1alpha1.WorkspaceExportReference]*apisv1alpha1.APIBinding{}

	// Check for any naming conflicts
	for i := range bindings {
		binding := bindings[i]

		if binding.Spec.Reference.Workspace == nil {
			continue
		}

		// Track what we have ("actual")
		exportToBinding[*binding.Spec.Reference.Workspace] = binding

		// Record any conflicts
		for _, cond := range binding.Status.Conditions {
			if cond.Reason == apisv1alpha1.NamingConflictsReason {
				logger.V(2).Info("found an APIBinding with conflicts", "message", cond.Message)

				exportRef := tenancyv1alpha1.APIExportReference{
					WorkspacePath: binding.Spec.Reference.Workspace.Path,
					Name:          binding.Spec.Reference.Workspace.ExportName,
				}

				conflicts, ok := bindingsWithConflicts[exportRef]
				if !ok {
					conflicts = sets.NewString()
					bindingsWithConflicts[exportRef] = conflicts
				}

				conflicts.Insert(binding.Name)

				continue
			}
		}
	}

	requiredExportRefs := map[tenancyv1alpha1.APIExportReference]struct{}{}

	// Get what we've already created for this workspace (from previous reconcile iteration(s) ). Note that even though
	// this returns a pointer to the set and not a copy, we are safe to mutate the contents outside the lock because
	// only 1 worker can ever be working on this workspace at a time.
	createdBindings := func() map[tenancyv1alpha1.APIExportReference]struct{} {
		b.createdBindingsLock.Lock()
		defer b.createdBindingsLock.Unlock()

		createdBindings := b.createdBindings[clusterName]
		if createdBindings == nil {
			createdBindings = make(map[tenancyv1alpha1.APIExportReference]struct{})
			b.createdBindings[clusterName] = createdBindings
		}

		return createdBindings
	}()

	for _, cwt := range cwts {
		logger := logging.WithObject(logger, cwt)
		logger.V(2).Info("attempting to initialize APIBindings")

		for i := range cwt.Spec.DefaultAPIBindings {
			exportRef := cwt.Spec.DefaultAPIBindings[i]

			// Keep track of unique set of expected exports across all CWTs
			requiredExportRefs[exportRef] = struct{}{}

			logger := logger.WithValues("apiExportPath", exportRef.WorkspacePath, "apiExportName", exportRef.Name)
			ctx := klog.NewContext(ctx, logger)

			for _, conflict := range bindingsWithConflicts[exportRef].UnsortedList() {
				logger.V(4).Info("attempting to delete APIBinding with conflicts", "apiBindingName", conflict)
				err = b.deleteAPIBinding(ctx, clusterName, conflict)
				if err != nil {
					logger.Error(err, "error deleting conflicting APIBinding", "apiBindingName", conflict)
				}
			}

			// Don't create if it's in the tracker
			if _, exists := createdBindings[exportRef]; exists {
				logger.V(4).Info("APIBinding already created for this export - skipping")
				continue
			}

			// Make sure the APIExport exists
			if _, err := b.getAPIExport(logicalcluster.New(exportRef.WorkspacePath), exportRef.Name); err != nil {
				errors = append(errors, err)
				continue
			}

			logger.V(2).Info("creating APIBinding")
			binding, err := b.createAPIBinding(ctx, clusterName,
				&apisv1alpha1.APIBinding{
					ObjectMeta: metav1.ObjectMeta{
						// TODO don't exceed max name len
						GenerateName: exportRef.Name + "-",
					},
					Spec: apisv1alpha1.APIBindingSpec{
						Reference: apisv1alpha1.ExportReference{
							Workspace: &apisv1alpha1.WorkspaceExportReference{
								Path:       exportRef.WorkspacePath,
								ExportName: exportRef.Name,
							},
						},
					},
				})

			if err != nil {
				errors = append(errors, err)
				continue
			}

			logging.WithObject(klog.FromContext(ctx), binding).V(2).Info("created APIBinding")

			// Track successful creations so we reduce conflicts
			createdBindings[exportRef] = struct{}{}
		}
	}

	if len(errors) > 0 {
		logger.Error(utilerrors.NewAggregate(errors), "error initializing APIBindings")

		conditions.MarkFalse(
			clusterWorkspace,
			tenancyv1alpha1.WorkspaceAPIBindingsInitialized,
			tenancyv1alpha1.WorkspaceInitializedAPIBindingErrors,
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
			Path:       exportRef.WorkspacePath,
			ExportName: exportRef.Name,
		}

		binding, exists := exportToBinding[workspaceExportRef]
		if !exists {
			incomplete = append(incomplete, fmt.Sprintf("for APIExport %s|%s", exportRef.WorkspacePath, exportRef.Name))
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
			tenancyv1alpha1.WorkspaceInitialized,
			tenancyv1alpha1.WorkspaceInitializedWaitingOnAPIBindings,
			conditionsv1alpha1.ConditionSeverityInfo,
			"APIBinding(s) not yet fully bound: %s", strings.Join(incomplete, ", "),
		)

		return nil
	}

	// Remove the bindings for the current workspace from the tracker
	b.createdBindingsLock.Lock()
	defer b.createdBindingsLock.Unlock()
	delete(b.createdBindings, clusterName)

	clusterWorkspace.Status.Initializers = initialization.EnsureInitializerAbsent(tenancyv1alpha1.ClusterWorkspaceAPIBindingsInitializer, clusterWorkspace.Status.Initializers)

	return nil
}
