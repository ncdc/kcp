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

package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	kcpclient "github.com/kcp-dev/kcp/pkg/client/clientset/versioned"
	"github.com/kcp-dev/kcp/pkg/client/informers/externalversions"
	"github.com/kcp-dev/kcp/pkg/cluster"
	"github.com/kcp-dev/kcp/pkg/controllerz"
	"github.com/kcp-dev/kcp/pkg/reconciler/deployment"
)

const numThreads = 2

var kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig")
var kubecontext = flag.String("context", "", "Context to use in the Kubeconfig file, instead of the current context")

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	flag.Parse()

	controllerz.EnableLogicalClusters()

	var overrides clientcmd.ConfigOverrides
	if *kubecontext != "" {
		overrides.CurrentContext = *kubecontext
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: *kubeconfig},
		&overrides,
	).ClientConfig()
	if err != nil {
		klog.Fatal(err)
	}

	crossKubeClient, err := kubernetes.NewClusterForConfig(cfg)
	if err != nil {
		panic(err)
	}

	crossKCPClient, err := kcpclient.NewClusterForConfig(cfg)
	if err != nil {
		panic(err)
	}
	// TODO: make a custom rest.HTTPClient that always does "*"
	kcpSharedInformerFactory := externalversions.NewSharedInformerFactoryWithOptions(crossKCPClient.Cluster("*"), 0)

	restClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		panic(err)
	}
	clusterAwareHTTPClient := cluster.NewHTTPClient(restClient)
	kubeClient, err := kubernetes.NewForConfigAndClient(cfg, clusterAwareHTTPClient)

	// TODO: make a custom rest.HTTPClient that always does "*"
	kubeSharedInformerFactory := informers.NewSharedInformerFactoryWithOptions(crossKubeClient.Cluster("*"), 0)

	deploymentController := deployment.NewController(
		kcpSharedInformerFactory.Cluster().V1alpha1().Clusters(),
		crossKubeClient,
		kubeClient.AppsV1(),
		kubeSharedInformerFactory.Apps().V1().Deployments(),
		kcpSharedInformerFactory.Cluster().V1alpha1().Clusters().Informer().HasSynced,
		kubeSharedInformerFactory.Apps().V1().Deployments().Informer().HasSynced,
	)

	kubeSharedInformerFactory.Start(ctx.Done())
	kcpSharedInformerFactory.Start(ctx.Done())

	deploymentController.Start(ctx, numThreads)
}
