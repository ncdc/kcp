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

package workspaceshard

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubernetesclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	kcpclientset "github.com/kcp-dev/kcp/pkg/client/clientset/versioned"
	tenancyv1alpha1client "github.com/kcp-dev/kcp/pkg/client/clientset/versioned/typed/tenancy/v1alpha1"
	"github.com/kcp-dev/kcp/pkg/controllerz"
	"github.com/kcp-dev/kcp/test/e2e/framework"
	utilconditions "github.com/kcp-dev/kcp/third_party/conditions/util/conditions"
)

func TestWorkspaceShardController(t *testing.T) {
	type runningServer struct {
		framework.RunningServer
		client     tenancyv1alpha1client.WorkspaceShardInterface
		kubeClient kubernetesclientset.Interface
		expect     framework.RegisterWorkspaceShardExpectation
	}
	var testCases = []struct {
		name string
		work func(ctx context.Context, t framework.TestingTInterface, server runningServer)
	}{
		{
			name: "create a workspace shard without credentials, expect to see status reflect missing credentials",
			work: func(ctx context.Context, t framework.TestingTInterface, server runningServer) {
				workspaceShard, err := server.client.Create(ctx, &tenancyv1alpha1.WorkspaceShard{ObjectMeta: metav1.ObjectMeta{Name: "of-glass"}}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create workspace shard: %v", err)
					return
				}
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.client.Get(ctx, workspaceShard.Name, metav1.GetOptions{})
				})
				if err := server.expect(workspaceShard, func(workspaceShard *tenancyv1alpha1.WorkspaceShard) error {
					if !utilconditions.IsFalse(workspaceShard, tenancyv1alpha1.WorkspaceShardCredentialsValid) || utilconditions.GetReason(workspaceShard, tenancyv1alpha1.WorkspaceShardCredentialsValid) != tenancyv1alpha1.WorkspaceShardCredentialsReasonMissing {
						return fmt.Errorf("expected to see missing credentials, got: %#v", workspaceShard.Status.Conditions)
					}
					return nil
				}); err != nil {
					t.Errorf("did not see workspace shard updated: %v", err)
					return
				}
			},
		},
		{
			name: "create a workspace shard referencing missing credentials, expect to see status reflect missing credentials",
			work: func(ctx context.Context, t framework.TestingTInterface, server runningServer) {
				workspaceShard, err := server.client.Create(ctx, &tenancyv1alpha1.WorkspaceShard{
					ObjectMeta: metav1.ObjectMeta{Name: "of-glass"},
					Spec: tenancyv1alpha1.WorkspaceShardSpec{Credentials: corev1.SecretReference{
						Name:      "not",
						Namespace: "real",
					}},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create workspace shard: %v", err)
					return
				}
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.client.Get(ctx, workspaceShard.Name, metav1.GetOptions{})
				})
				if err := server.expect(workspaceShard, func(workspaceShard *tenancyv1alpha1.WorkspaceShard) error {
					if !utilconditions.IsFalse(workspaceShard, tenancyv1alpha1.WorkspaceShardCredentialsValid) || utilconditions.GetReason(workspaceShard, tenancyv1alpha1.WorkspaceShardCredentialsValid) != tenancyv1alpha1.WorkspaceShardCredentialsReasonMissing {
						return fmt.Errorf("expected to see missing credentials, got: %#v", workspaceShard.Status.Conditions)
					}
					return nil
				}); err != nil {
					t.Errorf("did not see workspace shard updated: %v", err)
					return
				}
			},
		},
		{
			name: "create a workspace shard referencing credentials without data, expect to see status reflect invalid credentials",
			work: func(ctx context.Context, t framework.TestingTInterface, server runningServer) {
				if _, err := server.kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "credentials"}}, metav1.CreateOptions{}); err != nil {
					t.Errorf("failed to create credentials namespace: %v", err)
					return
				}
				secret, err := server.kubeClient.CoreV1().Secrets("credentials").Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "kubeconfig"},
					Data: map[string][]byte{
						"unrelated": []byte(`information`),
					},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create credentials secret: %v", err)
					return
				}
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.kubeClient.CoreV1().Secrets(secret.Namespace).Get(ctx, secret.Name, metav1.GetOptions{})
				})
				workspaceShard, err := server.client.Create(ctx, &tenancyv1alpha1.WorkspaceShard{
					ObjectMeta: metav1.ObjectMeta{Name: "of-glass"},
					Spec: tenancyv1alpha1.WorkspaceShardSpec{Credentials: corev1.SecretReference{
						Name:      "kubeconfig",
						Namespace: "credentials",
					}},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create workspace shard: %v", err)
					return
				}
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.client.Get(ctx, workspaceShard.Name, metav1.GetOptions{})
				})
				if err := server.expect(workspaceShard, func(workspaceShard *tenancyv1alpha1.WorkspaceShard) error {
					if !utilconditions.IsFalse(workspaceShard, tenancyv1alpha1.WorkspaceShardCredentialsValid) || utilconditions.GetReason(workspaceShard, tenancyv1alpha1.WorkspaceShardCredentialsValid) != tenancyv1alpha1.WorkspaceShardCredentialsReasonInvalid {
						return fmt.Errorf("expected to see invalid credentials, got: %#v", workspaceShard.Status.Conditions)
					}
					return nil
				}); err != nil {
					t.Errorf("did not see workspace shard updated: %v", err)
					return
				}
			},
		},
		{
			name: "create a workspace shard referencing credentials with invalid data, expect to see status reflect invalid credentials",
			work: func(ctx context.Context, t framework.TestingTInterface, server runningServer) {
				if _, err := server.kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "credentials"}}, metav1.CreateOptions{}); err != nil {
					t.Errorf("failed to create credentials namespace: %v", err)
					return
				}
				secret, err := server.kubeClient.CoreV1().Secrets("credentials").Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "kubeconfig"},
					Data: map[string][]byte{
						"kubeconfig": []byte(`not a kubeconfig`),
					},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create credentials secret: %v", err)
					return
				}
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.kubeClient.CoreV1().Secrets(secret.Namespace).Get(ctx, secret.Name, metav1.GetOptions{})
				})
				workspaceShard, err := server.client.Create(ctx, &tenancyv1alpha1.WorkspaceShard{
					ObjectMeta: metav1.ObjectMeta{Name: "of-glass"},
					Spec: tenancyv1alpha1.WorkspaceShardSpec{Credentials: corev1.SecretReference{
						Name:      "kubeconfig",
						Namespace: "credentials",
					}},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create workspace shard: %v", err)
					return
				}
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.client.Get(ctx, workspaceShard.Name, metav1.GetOptions{})
				})
				if err := server.expect(workspaceShard, func(workspaceShard *tenancyv1alpha1.WorkspaceShard) error {
					if !utilconditions.IsFalse(workspaceShard, tenancyv1alpha1.WorkspaceShardCredentialsValid) || utilconditions.GetReason(workspaceShard, tenancyv1alpha1.WorkspaceShardCredentialsValid) != tenancyv1alpha1.WorkspaceShardCredentialsReasonInvalid {
						return fmt.Errorf("expected to see invalid credentials, got: %#v", workspaceShard.Status.Conditions)
					}
					return nil
				}); err != nil {
					t.Errorf("did not see workspace shard updated: %v", err)
					return
				}
			},
		},
		{
			name: "create a workspace shard referencing valid credentials, expect to see status reflect that",
			work: func(ctx context.Context, t framework.TestingTInterface, server runningServer) {
				if _, err := server.kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "credentials"}}, metav1.CreateOptions{}); err != nil {
					t.Errorf("failed to create credentials namespace: %v", err)
					return
				}
				rawCfg := clientcmdapi.Config{
					Clusters:       map[string]*clientcmdapi.Cluster{"cluster": {Server: "https://kcp.dev/apiprefix"}},
					Contexts:       map[string]*clientcmdapi.Context{"context": {Cluster: "cluster", AuthInfo: "user"}},
					CurrentContext: "context",
					AuthInfos:      map[string]*clientcmdapi.AuthInfo{"user": {Username: "user", Password: "password"}},
				}
				rawBytes, err := clientcmd.Write(rawCfg)
				if err != nil {
					t.Errorf("could not serialize raw config: %v", err)
					return
				}
				cfg, err := clientcmd.NewNonInteractiveClientConfig(rawCfg, "context", nil, nil).ClientConfig()
				if err != nil {
					t.Errorf("failed to create client config: %v", err)
					return
				}
				secret, err := server.kubeClient.CoreV1().Secrets("credentials").Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "kubeconfig"},
					Data: map[string][]byte{
						"kubeconfig": rawBytes,
					},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create credentials secret: %v", err)
					return
				}
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.kubeClient.CoreV1().Secrets(secret.Namespace).Get(ctx, secret.Name, metav1.GetOptions{})
				})
				workspaceShard, err := server.client.Create(ctx, &tenancyv1alpha1.WorkspaceShard{
					ObjectMeta: metav1.ObjectMeta{Name: "of-glass"},
					Spec: tenancyv1alpha1.WorkspaceShardSpec{Credentials: corev1.SecretReference{
						Name:      "kubeconfig",
						Namespace: "credentials",
					}},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create workspace shard: %v", err)
					return
				}
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.client.Get(ctx, workspaceShard.Name, metav1.GetOptions{})
				})
				if err := server.expect(workspaceShard, func(workspaceShard *tenancyv1alpha1.WorkspaceShard) error {
					if !utilconditions.IsTrue(workspaceShard, tenancyv1alpha1.WorkspaceShardCredentialsValid) {
						return fmt.Errorf("expected to see valid credentials, got: %#v", workspaceShard.Status.Conditions)
					}
					if diff := cmp.Diff(workspaceShard.Status.ConnectionInfo, &tenancyv1alpha1.ConnectionInfo{
						Host:    cfg.Host,
						APIPath: cfg.APIPath,
					}); diff != "" {
						return fmt.Errorf("got incorrect connection info: %v", diff)
					}
					return nil
				}); err != nil {
					t.Errorf("did not see workspace shard updated: %v", err)
					return
				}
			},
		},
		{
			name: "update credentials for shard, see credential version change",
			work: func(ctx context.Context, t framework.TestingTInterface, server runningServer) {
				if _, err := server.kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "credentials"}}, metav1.CreateOptions{}); err != nil {
					t.Errorf("failed to create credentials namespace: %v", err)
					return
				}
				rawCfg := clientcmdapi.Config{
					Clusters:       map[string]*clientcmdapi.Cluster{"cluster": {Server: "https://kcp.dev/apiprefix"}},
					Contexts:       map[string]*clientcmdapi.Context{"context": {Cluster: "cluster", AuthInfo: "user"}},
					CurrentContext: "context",
					AuthInfos:      map[string]*clientcmdapi.AuthInfo{"user": {Username: "user", Password: "password"}},
				}
				rawBytes, err := clientcmd.Write(rawCfg)
				if err != nil {
					t.Errorf("could not serialize raw config: %v", err)
					return
				}
				cfg, err := clientcmd.NewNonInteractiveClientConfig(rawCfg, "context", nil, nil).ClientConfig()
				if err != nil {
					t.Errorf("failed to create client config: %v", err)
					return
				}
				secret, err := server.kubeClient.CoreV1().Secrets("credentials").Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "kubeconfig"},
					Data: map[string][]byte{
						"kubeconfig": rawBytes,
					},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create credentials secret: %v", err)
					return
				}
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.kubeClient.CoreV1().Secrets(secret.Namespace).Get(ctx, secret.Name, metav1.GetOptions{})
				})
				workspaceShard, err := server.client.Create(ctx, &tenancyv1alpha1.WorkspaceShard{
					ObjectMeta: metav1.ObjectMeta{Name: "of-glass"},
					Spec: tenancyv1alpha1.WorkspaceShardSpec{Credentials: corev1.SecretReference{
						Name:      "kubeconfig",
						Namespace: "credentials",
					}},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("failed to create workspace shard: %v", err)
					return
				}
				defer server.Artifact(t, func() (runtime.Object, error) {
					return server.client.Get(ctx, workspaceShard.Name, metav1.GetOptions{})
				})
				var version string
				if err := server.expect(workspaceShard, func(workspaceShard *tenancyv1alpha1.WorkspaceShard) error {
					if !utilconditions.IsTrue(workspaceShard, tenancyv1alpha1.WorkspaceShardCredentialsValid) {
						return fmt.Errorf("expected to see valid credentials, got: %#v", workspaceShard.Status.Conditions)
					}
					if diff := cmp.Diff(workspaceShard.Status.ConnectionInfo, &tenancyv1alpha1.ConnectionInfo{
						Host:    cfg.Host,
						APIPath: cfg.APIPath,
					}); diff != "" {
						return fmt.Errorf("got incorrect connection info: %v", diff)
					}
					if workspaceShard.Status.CredentialsHash == "" {
						return errors.New("no credential version encoded")
					}
					version = workspaceShard.Status.CredentialsHash
					return nil
				}); err != nil {
					t.Errorf("did not see workspace shard updated: %v", err)
					return
				}
				rawCfg.AuthInfos["user"].Password = "rotated"
				rawBytes, err = clientcmd.Write(rawCfg)
				if err != nil {
					t.Errorf("could not serialize raw config: %v", err)
					return
				}
				secret.Data["kubeconfig"] = rawBytes
				if _, err := server.kubeClient.CoreV1().Secrets("credentials").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
					t.Errorf("failed to create credentials secret: %v", err)
					return
				}
				if err := server.expect(workspaceShard, func(workspaceShard *tenancyv1alpha1.WorkspaceShard) error {
					if !utilconditions.IsTrue(workspaceShard, tenancyv1alpha1.WorkspaceShardCredentialsValid) {
						return fmt.Errorf("expected to see valid credentials, got: %#v", workspaceShard.Status.Conditions)
					}
					if workspaceShard.Status.CredentialsHash == version {
						return errors.New("did not see credential version change")
					}
					return nil
				}); err != nil {
					t.Errorf("did not see workspace shard updated: %v", err)
					return
				}
			},
		},
	}
	const serverName = "main"
	for i := range testCases {
		testCase := testCases[i]
		framework.Run(t, testCase.name, func(t framework.TestingTInterface, servers map[string]framework.RunningServer, artifactDir, dataDir string) {
			ctx := context.Background()
			if deadline, ok := t.Deadline(); ok {
				withDeadline, cancel := context.WithDeadline(ctx, deadline)
				t.Cleanup(cancel)
				ctx = withDeadline
			}
			if len(servers) != 1 {
				t.Errorf("incorrect number of servers: %d", len(servers))
				return
			}
			server := servers[serverName]
			cfg, err := server.Config()
			if err != nil {
				t.Error(err)
				return
			}
			clusterName := "admin"
			kcpClients, err := kcpclientset.NewScoperForConfig(cfg)
			if err != nil {
				t.Errorf("failed to construct kcp client for server: %v", err)
				return
			}
			scope := controllerz.NewScope(clusterName)
			kcpClient := kcpClients.Scope(scope)
			expect, err := framework.ExpectWorkspaceShards(ctx, t, kcpClient)
			if err != nil {
				t.Errorf("failed to start expecter: %v", err)
				return
			}
			kubeClients, err := kubernetesclientset.NewScoperForConfig(cfg)
			if err != nil {
				t.Errorf("failed to construct kube client for server: %v", err)
				return
			}
			kubeClient := kubeClients.Scope(scope)
			testCase.work(ctx, t, runningServer{
				RunningServer: server,
				client:        kcpClient.TenancyV1alpha1().WorkspaceShards(),
				kubeClient:    kubeClient,
				expect:        expect,
			})
		}, framework.KcpConfig{
			Name: serverName,
			Args: []string{"--install-workspace-controller"},
		})
	}
}
