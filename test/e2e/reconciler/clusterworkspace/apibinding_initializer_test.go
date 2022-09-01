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

package clusterworkspace

import (
	"context"
	"testing"

	"github.com/kcp-dev/logicalcluster/v2"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	kcpclient "github.com/kcp-dev/kcp/pkg/client/clientset/versioned"
	"github.com/kcp-dev/kcp/test/e2e/framework"
)

func TestClusterWorkspaceTypeAPIBindingInitialization(t *testing.T) {
	t.Parallel()

	server := framework.SharedKcpServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	orgClusterName := framework.NewOrganizationFixture(t, server)
	universal := framework.NewWorkspaceFixture(t, server, orgClusterName, framework.WithName("universal"))

	cwtParent1 := &tenancyv1alpha1.ClusterWorkspaceType{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parent1",
		},
		Spec: tenancyv1alpha1.ClusterWorkspaceTypeSpec{
			DefaultAPIBindings: []tenancyv1alpha1.APIExportReference{
				{
					WorkspacePath: "root",
					Name:          "tenancy.kcp.dev",
				},
				{
					WorkspacePath: "root",
					Name:          "scheduling.kcp.dev",
				},
			},
		},
	}

	cwtParent2 := &tenancyv1alpha1.ClusterWorkspaceType{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parent2",
		},
		Spec: tenancyv1alpha1.ClusterWorkspaceTypeSpec{
			DefaultAPIBindings: []tenancyv1alpha1.APIExportReference{
				{
					WorkspacePath: "root",
					Name:          "workload.kcp.dev",
				},
			},
		},
	}

	cwt := &tenancyv1alpha1.ClusterWorkspaceType{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: tenancyv1alpha1.ClusterWorkspaceTypeSpec{
			DefaultAPIBindings: []tenancyv1alpha1.APIExportReference{
				{
					WorkspacePath: "root",
					Name:          "shards.tenancy.kcp.dev",
				},
			},
			Extend: tenancyv1alpha1.ClusterWorkspaceTypeExtension{
				With: []tenancyv1alpha1.ClusterWorkspaceTypeReference{
					{
						Name: "parent1",
						Path: universal.String(),
					},
					{
						Name: "parent2",
						Path: universal.String(),
					},
				},
			},
		},
	}

	cfg := server.BaseConfig(t)

	kcpClusterClient, err := kcpclient.NewForConfig(cfg)
	require.NoError(t, err, "error creating kcp cluster client")

	_, err = kcpClusterClient.TenancyV1alpha1().ClusterWorkspaceTypes().Create(logicalcluster.WithCluster(ctx, universal), cwtParent1, metav1.CreateOptions{})
	require.NoError(t, err, "error creating cwt parent1")

	_, err = kcpClusterClient.TenancyV1alpha1().ClusterWorkspaceTypes().Create(logicalcluster.WithCluster(ctx, universal), cwtParent2, metav1.CreateOptions{})
	require.NoError(t, err, "error creating cwt parent2")

	_, err = kcpClusterClient.TenancyV1alpha1().ClusterWorkspaceTypes().Create(logicalcluster.WithCluster(ctx, universal), cwt, metav1.CreateOptions{})
	require.NoError(t, err, "error creating cwt test")

	// This will create and wait for ready, which only happens if the APIBinding initialization is working correctly
	_ = framework.NewWorkspaceFixture(t, server, universal, framework.WithType(universal, "test"), framework.WithName("init"))
}
