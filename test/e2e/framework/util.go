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

package framework

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	cacheddiscovery "k8s.io/client-go/discovery/cached"
	kubernetesclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kcp-dev/kcp/config"
	apiresourcev1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apiresource/v1alpha1"
	clusterv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/cluster/v1alpha1"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	kcpclientset "github.com/kcp-dev/kcp/pkg/client/clientset/versioned"
	"github.com/kcp-dev/kcp/pkg/controllerz"
)

// ScratchDirs determines where artifacts and data should live for a test case.
func ScratchDirs(t TestingTInterface) (string, string, error) {
	var baseDir string
	if dir, set := os.LookupEnv("ARTIFACT_DIR"); set {
		baseDir = dir
	} else {
		baseDir = t.TempDir()
	}
	baseDir = filepath.Join(baseDir, strings.NewReplacer("\\", "_", ":", "_").Replace(t.Name()))
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", "", fmt.Errorf("could not create base dir: %w", err)
	}
	baseTempDir, err := os.MkdirTemp(baseDir, "")
	if err != nil {
		return "", "", fmt.Errorf("could not create base temp dir: %w", err)
	}
	var directories []string
	for _, prefix := range []string{"artifacts", "data"} {
		dir := filepath.Join(baseTempDir, prefix)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", "", fmt.Errorf("could not create subdir: %w", err)
		}
		directories = append(directories, dir)
	}
	t.Logf("Saving test artifacts and data under %s.", baseTempDir)
	return directories[0], directories[1], nil
}

var localSchemeBuilder = runtime.SchemeBuilder{
	apiresourcev1alpha1.AddToScheme,
	tenancyv1alpha1.AddToScheme,
	clusterv1alpha1.AddToScheme,
}

func init() {
	utilruntime.Must(localSchemeBuilder.AddToScheme(scheme.Scheme))
}

// Artifact runs the data-producing function and dumps the YAML-formatted output
// to the artifact directory for the test. Normal usage looks like:
// defer framework.Artifact(t, kcp, client.Get(ctx, name, metav1.GetOptions{}))
func (c *kcpServer) Artifact(tinterface TestingTInterface, producer func() (runtime.Object, error)) {
	t, ok := tinterface.(*T)
	if !ok {
		tinterface.Logf("Artifact() called with %#v, not a framework.T", tinterface)
		return
	}
	data, err := producer()
	if err != nil {
		t.Logf("error fetching artifact: %v", err)
		return
	}
	accessor, ok := data.(metav1.Object)
	if !ok {
		t.Logf("artifact has no object meta: %#v", data)
		return
	}
	dir := path.Join(c.artifactDir, accessor.GetClusterName())
	if accessor.GetNamespace() != "" {
		dir = path.Join(dir, accessor.GetNamespace())
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Logf("could not create dir: %v", err)
		return
	}
	gvks, _, err := scheme.Scheme.ObjectKinds(data)
	if err != nil {
		t.Logf("error finding gvk for artifact: %v", err)
		return
	}
	if len(gvks) == 0 {
		t.Logf("found no gvk for artifact: %T", data)
		return
	}
	gvk := gvks[0]
	data.GetObjectKind().SetGroupVersionKind(gvk)

	cfg, err := c.Config()
	if err != nil {
		t.Logf("could not get config for server: %v", err)
		return
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		t.Logf("could not get discovery client for server: %v", err)
		return
	}
	scope := controllerz.NewScope(accessor.GetClusterName())
	scopedDiscoveryClient := discoveryClient.Scope(scope)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cacheddiscovery.NewMemCacheClient(scopedDiscoveryClient))
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		t.Logf("could not get REST mapping for artifact's GVK: %v", err)
		return
	}
	file := path.Join(dir, fmt.Sprintf("%s_%s.yaml", mapping.Resource.GroupResource().String(), accessor.GetName()))
	t.Logf("saving artifact to %s", file)

	serializer := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme, json.SerializerOptions{Yaml: true})
	raw := bytes.Buffer{}
	if err := serializer.Encode(data, &raw); err != nil {
		t.Logf("error marshalling artifact: %v", err)
		return
	}
	if err := ioutil.WriteFile(file, raw.Bytes(), 0644); err != nil {
		t.Logf("error writing artifact: %v", err)
		return
	}
}

// GetFreePort asks the kernel for a free open port that is ready to use.
func GetFreePort(t TestingTInterface) (string, error) {
	for {
		addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
		if err != nil {
			return "", fmt.Errorf("could not resolve free port: %w", err)
		}

		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return "", fmt.Errorf("could not listen on free port: %w", err)
		}
		defer func(c io.Closer) {
			if err := c.Close(); err != nil {
				t.Errorf("could not close listener: %v", err)
			}
		}(l)
		port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
		// Tests run in -parallel will run in separate processes, so we must use the file-system
		// for sharing state and locking across them to coordinate who gets which port. Without
		// some mechanism for sharing state, the following race is possible:
		// - process A calls net.ListenTCP() to resolve a new port
		// - process A calls l.Close() to close the listener to allow accessory to use it
		// - process B calls net.ListenTCP() and resolves the same port
		// - process A attempts to use the port, fails as it is in use
		// Therefore, holding the listener open is our kernel-based lock for this process, and while
		// we hold it open we must record our intent to disk.
		lockDir := filepath.Join(os.TempDir(), "kcp-e2e-ports")
		if err := os.MkdirAll(lockDir, 0755); err != nil {
			return "", fmt.Errorf("could not create port lockfile dir: %w", err)
		}
		lockFile := filepath.Join(lockDir, port)
		if _, err := os.Stat(lockFile); os.IsNotExist(err) {
			// we've never seen this before, we can use it
			f, err := os.Create(lockFile)
			if err != nil {
				return "", fmt.Errorf("could not record port lockfile: %w", err)
			}
			if err := f.Close(); err != nil {
				return "", fmt.Errorf("could not close port lockfile: %w", err)
			}
			// the lifecycle of an accessory (and thereby its ports) is the test lifecycle
			t.Cleanup(func() {
				if err := os.Remove(lockFile); err != nil {
					t.Errorf("failed to remove port lockfile: %v", err)
				}
			})
			return port, nil
		} else if err != nil {
			return "", fmt.Errorf("could not access port lockfile: %w", err)
		}
		t.Logf("found a previously-seen port, retrying: %s", port)
	}
}

// InstallCrd installs a CRD on one or multiple servers.
func InstallCrd(ctx context.Context, gvk metav1.GroupKind, servers map[string]RunningServer, embeddedResources embed.FS) error {
	wg := sync.WaitGroup{}
	bootstrapErrChan := make(chan error, len(servers))
	for _, server := range servers {
		wg.Add(1)
		go func(server RunningServer) {
			defer wg.Done()
			cfg, err := server.Config()
			if err != nil {
				bootstrapErrChan <- err
				return
			}
			crdClient, err := apiextensionsv1client.NewForConfig(cfg)
			if err != nil {
				bootstrapErrChan <- fmt.Errorf("failed to construct client for server: %w", err)
				return
			}
			bootstrapErrChan <- config.BootstrapCustomResourceDefinitionFromFS(ctx, crdClient.CustomResourceDefinitions(), gvk, embeddedResources)
		}(server)
	}
	wg.Wait()
	close(bootstrapErrChan)
	var bootstrapErrors []error
	for err := range bootstrapErrChan {
		bootstrapErrors = append(bootstrapErrors, err)
	}
	if err := kerrors.NewAggregate(bootstrapErrors); err != nil {
		return fmt.Errorf("could not bootstrap CRDs: %w", err)
	}
	return nil
}

// InstallCluster creates a new Cluster resource with the desired name on a given server and waits for it to be ready.
func InstallCluster(t TestingTInterface, ctx context.Context, source, server RunningServer, crdName, clusterName string) error {
	sourceCfg, err := source.Config()
	if err != nil {
		return fmt.Errorf("failed to get source config: %w", err)
	}
	rawServerCfg, err := server.RawConfig()
	if err != nil {
		return fmt.Errorf("failed to get server config: %w", err)
	}
	sourceClusterName, err := DetectClusterName(sourceCfg, ctx, crdName)
	if err != nil {
		return fmt.Errorf("failed to detect cluster name: %w", err)
	}
	sourceKcpClients, err := kcpclientset.NewClusterForConfig(sourceCfg)
	if err != nil {
		return fmt.Errorf("failed to construct client for server: %w", err)
	}
	rawSinkCfgBytes, err := clientcmd.Write(rawServerCfg)
	if err != nil {
		return fmt.Errorf("failed to serialize server config: %w", err)
	}
	sourceKcpClient := sourceKcpClients.Cluster(sourceClusterName)
	cluster, err := sourceKcpClient.ClusterV1alpha1().Clusters().Create(ctx, &clusterv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		Spec:       clusterv1alpha1.ClusterSpec{KubeConfig: string(rawSinkCfgBytes)},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create cluster on source kcp: %w", err)
	}
	defer source.Artifact(t, func() (runtime.Object, error) {
		return sourceKcpClient.ClusterV1alpha1().Clusters().Get(ctx, cluster.Name, metav1.GetOptions{})
	})
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer func() {
		cancel()
	}()
	watcher, err := sourceKcpClient.ClusterV1alpha1().Clusters().Watch(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.name", cluster.Name).String(),
	})
	if err != nil {
		return fmt.Errorf("failed to watch cluster in source kcp: %w", err)
	}
	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("failed to wait for cluster in source kcp to be ready: %w", waitCtx.Err())
		case event := <-watcher.ResultChan():
			switch event.Type {
			case watch.Added, watch.Bookmark:
				continue
			case watch.Modified:
				updated, ok := event.Object.(*clusterv1alpha1.Cluster)
				if !ok {
					continue
				}
				var ready bool
				for _, condition := range updated.Status.Conditions {
					if condition.Type == clusterv1alpha1.ClusterConditionReady && condition.Status == corev1.ConditionTrue {
						ready = true
						break
					}
				}
				if ready {
					return nil
				}
			case watch.Deleted:
				return fmt.Errorf("cluster %s was deleted before being ready", cluster.Name)
			case watch.Error:
				return fmt.Errorf("encountered error while watching cluster %s: %#v", cluster.Name, event.Object)
			}
		}
	}
}

// InstallNamespace creates a new namespace into the desired server.
func InstallNamespace(ctx context.Context, server RunningServer, crdName, testNamespace string) error {
	client, err := GetClientForServer(ctx, server, crdName)
	if err != nil {
		return err
	}
	_, err = client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
	}, metav1.CreateOptions{})
	return err
}

// DetectClusterName returns the name of the cluster that contains the desired CRD.
// TODO: we need to undo the prefixing and get normal sharding behavior in soon ... ?
func DetectClusterName(cfg *rest.Config, ctx context.Context, crdName string) (string, error) {
	crdClient, err := apiextensionsclientset.NewClusterForConfig(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to construct client for server: %w", err)
	}
	crds, err := crdClient.Cluster("*").ApiextensionsV1().CustomResourceDefinitions().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to list crds: %w", err)
	}
	if len(crds.Items) == 0 {
		return "", errors.New("found no crds, cannot detect cluster name")
	}
	for _, crd := range crds.Items {
		if crd.ObjectMeta.Name == crdName {
			return crd.ObjectMeta.ClusterName, nil
		}
	}
	return "", errors.New("detected no admin cluster")
}

// GetClientForServer returns a kubernetes clientset for a given server.
func GetClientForServer(ctx context.Context, server RunningServer, crdName string) (kubernetesclientset.Interface, error) {
	cfg, err := server.Config()
	if err != nil {
		return nil, err
	}
	sourceClusterName, err := DetectClusterName(cfg, ctx, crdName)
	if err != nil {
		return nil, fmt.Errorf("failed to detect cluster name: %w", err)
	}
	clients, err := kubernetesclientset.NewClusterForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to construct client for server: %w", err)
	}
	client := clients.Cluster(sourceClusterName)
	return client, nil
}
