/*
Copyright 2021 The KCP Authors.

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

package workspaces

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	clientset "github.com/kcp-dev/kcp/pkg/client/clientset/versioned"
	"github.com/kcp-dev/kcp/pkg/controllerz"
	virtualcmd "github.com/kcp-dev/kcp/pkg/virtual/framework/cmd"
	workspacescmd "github.com/kcp-dev/kcp/pkg/virtual/workspaces/cmd"
	"github.com/kcp-dev/kcp/test/e2e/framework"
	"github.com/kcp-dev/kcp/test/e2e/virtual/helpers"
)

type testDataType struct {
	user1, user2, user3                                                      framework.User
	workspace1, workspace1Disambiguited, workspace2, workspace2Disambiguited *tenancyv1alpha1.Workspace
}

var testData testDataType = testDataType{
	user1: framework.User{
		Name:   "user-1",
		UID:    "1111-1111-1111-1111",
		Token:  "user-1-token",
		Groups: []string{"team-1"},
	},
	user2: framework.User{
		Name:   "user-2",
		UID:    "2222-2222-2222-2222",
		Token:  "user-2-token",
		Groups: []string{"team-2"},
	},
	user3: framework.User{
		Name:   "user-3",
		UID:    "3333-3333-3333-3333",
		Token:  "user-3-token",
		Groups: []string{"team-3"},
	},
	workspace1:              &tenancyv1alpha1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "workspace1"}},
	workspace1Disambiguited: &tenancyv1alpha1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "workspace1--1"}},
	workspace2:              &tenancyv1alpha1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "workspace2"}},
	workspace2Disambiguited: &tenancyv1alpha1.Workspace{ObjectMeta: metav1.ObjectMeta{Name: "workspace2--1"}},
}

func TestWorkspacesVirtualWorkspaces(t *testing.T) {
	type runningServer struct {
		framework.RunningServer
		kcpClient                      clientset.Interface
		kcpExpect                      framework.RegisterWorkspaceExpectation
		virtualWorkspaceClientContexts []helpers.VirtualWorkspaceClientContext
		virtualWorkspaceClients        []clientset.Interface
	}
	var testCases = []struct {
		name                           string
		virtualWorkspaceClientContexts []helpers.VirtualWorkspaceClientContext
		work                           func(ctx context.Context, t framework.TestingTInterface, server runningServer)
	}{
		{
			name: "create a workspace in personal virtual workspace and have only its owner list it",
			virtualWorkspaceClientContexts: []helpers.VirtualWorkspaceClientContext{
				{
					User:   testData.user1,
					Prefix: "/personal",
				},
				{
					User:   testData.user2,
					Prefix: "/personal",
				},
			},
			work: func(ctx context.Context, t framework.TestingTInterface, server runningServer) {
				vwUser1Client := server.virtualWorkspaceClients[0]
				vwUser2Client := server.virtualWorkspaceClients[1]
				workspace1, err := vwUser1Client.TenancyV1alpha1().Workspaces().Create(ctx, testData.workspace1, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create workspace: %v", err)
					return
				}
				assert.Equal(t, testData.workspace1.Name, workspace1.Name)
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.kcpClient.TenancyV1alpha1().Workspaces().Get(ctx, testData.workspace1.Name, metav1.GetOptions{})
				})
				workspace2, err := vwUser2Client.TenancyV1alpha1().Workspaces().Create(ctx, testData.workspace2, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create workspace: %v", err)
					return
				}
				assert.Equal(t, testData.workspace2.Name, workspace2.Name)
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.kcpClient.TenancyV1alpha1().Workspaces().Get(ctx, testData.workspace2.Name, metav1.GetOptions{})
				})
				if err := server.kcpExpect(workspace1, func(w *tenancyv1alpha1.Workspace) error {
					return nil
				}); err != nil {
					t.Errorf("did not see the workspace created in KCP: %v", err)
					return
				}
				if err := server.kcpExpect(workspace2, func(w *tenancyv1alpha1.Workspace) error {
					return nil
				}); err != nil {
					t.Errorf("did not see the workspace created in KCP: %v", err)
					return
				}
				if _, err := framework.WaitForCondition(t, ctx, 10*time.Second, func(c context.Context, t framework.TestingTInterface) (bool, error) {
					workspaceList, err := vwUser1Client.TenancyV1alpha1().Workspaces().List(ctx, metav1.ListOptions{})
					if err != nil {
						return false, err
					}
					if assert.Len(t, workspaceList.Items, 1) {
						return assert.Equal(t, workspace1.Name, workspaceList.Items[0].Name, "failed to create workspace"), nil
					}
					return false, nil
				}); err != nil {
					t.Errorf("failed to create workspace: %v", err)
				}
				if _, err := framework.WaitForCondition(t, ctx, 10*time.Second, func(c context.Context, t framework.TestingTInterface) (bool, error) {
					workspaceList, err := vwUser2Client.TenancyV1alpha1().Workspaces().List(ctx, metav1.ListOptions{})
					if err != nil {
						return false, err
					}
					if assert.Len(t, workspaceList.Items, 1) {
						return assert.Equal(t, workspace2.Name, workspaceList.Items[0].Name, "failed to create workspace"), nil
					}
					return false, nil
				}); err != nil {
					t.Errorf("failed to create workspace: %v", err)
				}
			},
		},
	}

	const serverName = "main"

	for i := range testCases {
		testCase := testCases[i]

		var users []framework.User
		for _, vwClientContexts := range testCase.virtualWorkspaceClientContexts {
			users = append(users, vwClientContexts.User)
		}
		usersKCPArgs, err := framework.Users(users).ArgsForKCP(t)
		if err != nil {
			t.Error(err)
			continue
		}

		framework.Run(t, testCase.name, func(t framework.TestingTInterface, servers map[string]framework.RunningServer, artifactDir, dataDir string) {
			if len(servers) != 1 {
				t.Errorf("incorrect number of servers: %d", len(servers))
				return
			}
			server := servers[serverName]

			ctx := context.Background()
			if deadline, ok := t.Deadline(); ok {
				withDeadline, cancel := context.WithDeadline(ctx, deadline)
				t.Cleanup(cancel)
				ctx = withDeadline
			}

			vw := helpers.VirtualWorkspace{
				BuildSubCommandOtions: func(kcpServer framework.RunningServer) virtualcmd.SubCommandOptions {
					return &workspacescmd.WorkspacesSubCommandOptions{
						KubeconfigFile: kcpServer.KubeconfigPath(),
						RootPathPrefix: "/",
					}
				},
				ClientContexts: testCase.virtualWorkspaceClientContexts,
			}

			vwConfigs, err := vw.Setup(t, ctx, server)
			if err != nil {
				t.Error(err.Error())
				return
			}

			virtualWorkspaceClients := []clientset.Interface{}
			for _, vwConfig := range vwConfigs {
				vwClients, err := clientset.NewForConfig(vwConfig)
				if err != nil {
					t.Errorf("failed to construct client for server: %v", err)
					return
				}
				virtualWorkspaceClients = append(virtualWorkspaceClients, vwClients)
			}

			kcpCfg, err := server.Config()
			if err != nil {
				t.Error(err)
				return
			}
			clusterName := "admin"
			scope := controllerz.NewScope(clusterName)
			kcpClients, err := clientset.NewScoperForConfig(kcpCfg)
			if err != nil {
				t.Errorf("failed to construct client for server: %v", err)
				return
			}
			kcpClient := kcpClients.Scope(scope)
			kcpExpect, err := framework.ExpectWorkspaces(ctx, t, kcpClient)
			if err != nil {
				t.Errorf("failed to start expecter: %v", err)
				return
			}

			testCase.work(ctx, t, runningServer{
				RunningServer:                  server,
				kcpClient:                      kcpClient,
				kcpExpect:                      kcpExpect,
				virtualWorkspaceClientContexts: testCase.virtualWorkspaceClientContexts,
				virtualWorkspaceClients:        virtualWorkspaceClients,
			})
		}, framework.KcpConfig{
			Name: serverName,
			Args: append([]string{"--install-workspace-controller"}, usersKCPArgs...),
		})
	}
}
