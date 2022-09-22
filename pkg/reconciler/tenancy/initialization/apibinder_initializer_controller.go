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
	"time"

	"github.com/go-logr/logr"
	"github.com/kcp-dev/logicalcluster/v2"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clusters"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	admission "github.com/kcp-dev/kcp/pkg/admission/clusterworkspacetypeexists"
	apisv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apis/v1alpha1"
	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	kcpclient "github.com/kcp-dev/kcp/pkg/client/clientset/versioned"
	apisinformer "github.com/kcp-dev/kcp/pkg/client/informers/externalversions/apis/v1alpha1"
	tenancyinformer "github.com/kcp-dev/kcp/pkg/client/informers/externalversions/tenancy/v1alpha1"
	"github.com/kcp-dev/kcp/pkg/indexers"
	"github.com/kcp-dev/kcp/pkg/logging"
	"github.com/kcp-dev/kcp/pkg/reconciler/committer"
)

const (
	ControllerName = "kcp-apibinder-initializer"
)

// NewAPIBinder returns a new controller which instantiates APIBindings and waits for them to be fully bound
// in new ClusterWorkspaces.
func NewAPIBinder(
	kcpClusterClient kcpclient.Interface,
	clusterWorkspaceInformer tenancyinformer.ClusterWorkspaceInformer,
	clusterWorkspaceTypeInformer tenancyinformer.ClusterWorkspaceTypeInformer,
	apiBindingsInformer apisinformer.APIBindingInformer,
	apiExportsInformer apisinformer.APIExportInformer,
) (*APIBinder, error) {
	c := &APIBinder{
		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), ControllerName),

		getClusterWorkspace: func(clusterName logicalcluster.Name) (*tenancyv1alpha1.ClusterWorkspace, error) {
			parent, workspace := clusterName.Split()
			return clusterWorkspaceInformer.Lister().Get(clusters.ToClusterAwareKey(parent, workspace))
		},
		getClusterWorkspaceType: func(clusterName logicalcluster.Name, name string) (*tenancyv1alpha1.ClusterWorkspaceType, error) {
			return clusterWorkspaceTypeInformer.Lister().Get(clusters.ToClusterAwareKey(clusterName, name))
		},
		listClusterWorkspaces: func() ([]*tenancyv1alpha1.ClusterWorkspace, error) {
			return clusterWorkspaceInformer.Lister().List(labels.Everything())
		},

		listAPIBindings: func(clusterName logicalcluster.Name) ([]*apisv1alpha1.APIBinding, error) {
			return indexers.ByIndex[*apisv1alpha1.APIBinding](apiBindingsInformer.Informer().GetIndexer(), indexers.ByLogicalCluster, clusterName.String())
		},
		getAPIBinding: func(clusterName logicalcluster.Name, name string) (*apisv1alpha1.APIBinding, error) {
			return apiBindingsInformer.Lister().Get(clusters.ToClusterAwareKey(clusterName, name))
		},
		createAPIBinding: func(ctx context.Context, clusterName logicalcluster.Name, binding *apisv1alpha1.APIBinding) (*apisv1alpha1.APIBinding, error) {
			return kcpClusterClient.ApisV1alpha1().APIBindings().Create(logicalcluster.WithCluster(ctx, clusterName), binding, metav1.CreateOptions{})
		},
		deleteAPIBinding: func(ctx context.Context, clusterName logicalcluster.Name, bindingName string) error {
			return kcpClusterClient.ApisV1alpha1().APIBindings().Delete(logicalcluster.WithCluster(ctx, clusterName), bindingName, metav1.DeleteOptions{})
		},

		getAPIExport: func(clusterName logicalcluster.Name, name string) (*apisv1alpha1.APIExport, error) {
			return apiExportsInformer.Lister().Get(clusters.ToClusterAwareKey(clusterName, name))
		},

		commit: committer.NewCommitter[*tenancyv1alpha1.ClusterWorkspace, *tenancyv1alpha1.ClusterWorkspaceSpec, *tenancyv1alpha1.ClusterWorkspaceStatus](kcpClusterClient.TenancyV1alpha1().ClusterWorkspaces()),
	}

	c.transitiveTypeResolver = admission.NewTransitiveTypeResolver(c.getClusterWorkspaceType)

	logger := logging.WithReconciler(klog.Background(), ControllerName)

	clusterWorkspaceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueClusterWorkspace(obj, logger)
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueueClusterWorkspace(obj, logger)
		},
	})

	apiBindingsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueAPIBinding(obj, logger)

		},
		UpdateFunc: func(_, obj interface{}) {
			c.enqueueAPIBinding(obj, logger)
		},
	})

	clusterWorkspaceTypeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueueClusterWorkspaceType(obj, logger)
		},
		UpdateFunc: func(_, obj interface{}) {
			c.enqueueClusterWorkspaceType(obj, logger)
		},
	})

	return c, nil
}

type clusterWorkspaceResource = committer.Resource[*tenancyv1alpha1.ClusterWorkspaceSpec, *tenancyv1alpha1.ClusterWorkspaceStatus]

// APIBinder is a controller which instantiates APIBindings and waits for them to be fully bound
// in new ClusterWorkspaces.
type APIBinder struct {
	queue workqueue.RateLimitingInterface

	getClusterWorkspace     func(clusterName logicalcluster.Name) (*tenancyv1alpha1.ClusterWorkspace, error)
	getClusterWorkspaceType func(clusterName logicalcluster.Name, name string) (*tenancyv1alpha1.ClusterWorkspaceType, error)
	listClusterWorkspaces   func() ([]*tenancyv1alpha1.ClusterWorkspace, error)

	listAPIBindings  func(clusterName logicalcluster.Name) ([]*apisv1alpha1.APIBinding, error)
	getAPIBinding    func(clusterName logicalcluster.Name, name string) (*apisv1alpha1.APIBinding, error)
	createAPIBinding func(ctx context.Context, clusterName logicalcluster.Name, binding *apisv1alpha1.APIBinding) (*apisv1alpha1.APIBinding, error)
	deleteAPIBinding func(ctx context.Context, clusterName logicalcluster.Name, bindingName string) error

	getAPIExport func(clusterName logicalcluster.Name, name string) (*apisv1alpha1.APIExport, error)

	transitiveTypeResolver transitiveTypeResolver

	// commit creates a patch and submits it, if needed.
	commit func(ctx context.Context, new, old *clusterWorkspaceResource) error
}

type transitiveTypeResolver interface {
	Resolve(t *tenancyv1alpha1.ClusterWorkspaceType) ([]*tenancyv1alpha1.ClusterWorkspaceType, error)
}

func (b *APIBinder) enqueueClusterWorkspace(obj interface{}, logger logr.Logger) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}

	logging.WithQueueKey(logger, key).V(2).Info("queueing ClusterWorkspace")
	b.queue.Add(key)
}

func (b *APIBinder) enqueueAPIBinding(obj interface{}, logger logr.Logger) {
	apiBinding, ok := obj.(*apisv1alpha1.APIBinding)
	if !ok {
		runtime.HandleError(fmt.Errorf("expected APIBinding, got %T", obj))
		return
	}

	logger = logging.WithObject(logger, apiBinding)

	clusterName := logicalcluster.From(apiBinding)
	clusterWorkspace, err := b.getClusterWorkspace(clusterName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// The workspace was deleted or is no longer initializing, so we can safely ignore this event.
			return
		}

		parent, workspace := clusterName.Split()
		logger.Error(err, "failed to get ClusterWorkspace from lister", "parent", parent, "workspace", workspace)
		return // nothing we can do here
	}

	b.enqueueClusterWorkspace(clusterWorkspace, logger)
}

// enqueueClusterWorkspaceType enqueues all clusterworkspaces (which are only those that are initializing, because of
// how the informer is supposed to be configured) whenever a clusterworkspacetype changes. If a clusterworkspacetype
// had a typo in the default set of apibindings, there is a chance the requeuing here would pick up a fix.
func (b *APIBinder) enqueueClusterWorkspaceType(obj interface{}, logger logr.Logger) {
	cwt, ok := obj.(*tenancyv1alpha1.ClusterWorkspaceType)
	if !ok {
		runtime.HandleError(fmt.Errorf("obj is supposed to be a ClusterWorkspaceType, but is %T", obj))
		return
	}

	if len(cwt.Spec.DefaultAPIBindings) == 0 {
		return
	}

	list, err := b.listClusterWorkspaces()
	if err != nil {
		runtime.HandleError(fmt.Errorf("error listing clusterworkspaces: %w", err))
	}

	for _, ws := range list {
		logger := logging.WithObject(logger, ws)
		b.enqueueClusterWorkspace(ws, logger)
	}
}

func (b *APIBinder) startWorker(ctx context.Context) {
	for b.processNextWorkItem(ctx) {
	}
}

func (b *APIBinder) Start(ctx context.Context, numThreads int) {
	defer runtime.HandleCrash()
	defer b.queue.ShutDown()
	logger := logging.WithReconciler(klog.FromContext(ctx), ControllerName)
	ctx = klog.NewContext(ctx, logger)

	logger.Info("Starting controller")
	defer logger.Info("Shutting down controller")

	for i := 0; i < numThreads; i++ {
		go wait.UntilWithContext(ctx, b.startWorker, time.Second)
	}
	<-ctx.Done()
}

func (b *APIBinder) ShutDown() {
	b.queue.ShutDown()
}

func (b *APIBinder) processNextWorkItem(ctx context.Context) bool {
	// Wait until there is a new item in the working queue
	k, quit := b.queue.Get()
	if quit {
		return false
	}
	key := k.(string)

	logger := logging.WithQueueKey(klog.FromContext(ctx), key)
	ctx = klog.NewContext(ctx, logger)
	logger.V(1).Info("processing key")

	// No matter what, tell the queue we're done with this key, to unblock
	// other workers.
	defer b.queue.Done(key)

	if err := b.process(ctx, key); err != nil {
		runtime.HandleError(fmt.Errorf("%s: failed to sync %q, err: %w", ControllerName, key, err))
		b.queue.AddRateLimited(key)
		return true
	}

	b.queue.Forget(key)
	return true
}

func (b *APIBinder) process(ctx context.Context, key string) error {
	logger := klog.FromContext(ctx)

	_, clusterAwareName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Error(err, "unable to decode key")
		return nil
	}

	parent, workspace := clusters.SplitClusterAwareKey(clusterAwareName)
	clusterName := parent.Join(workspace)

	clusterWorkspace, err := b.getClusterWorkspace(clusterName)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			logger.Error(err, "failed to get ClusterWorkspace from lister", "parentCluster", parent, "clusterWorkspace", workspace)
		}

		return nil // nothing we can do here
	}

	old := clusterWorkspace
	clusterWorkspace = clusterWorkspace.DeepCopy()

	logger = logging.WithObject(logger, clusterWorkspace)
	ctx = klog.NewContext(ctx, logger)

	var errs []error
	err = b.reconcile(ctx, clusterWorkspace)
	if err != nil {
		errs = append(errs, err)
	}

	// If the object being reconciled changed as a result, update it.
	oldResource := &clusterWorkspaceResource{ObjectMeta: old.ObjectMeta, Spec: &old.Spec, Status: &old.Status}
	newResource := &clusterWorkspaceResource{ObjectMeta: clusterWorkspace.ObjectMeta, Spec: &clusterWorkspace.Spec, Status: &clusterWorkspace.Status}
	if err := b.commit(ctx, oldResource, newResource); err != nil {
		errs = append(errs, err)
	}

	return utilerrors.NewAggregate(errs)
}
