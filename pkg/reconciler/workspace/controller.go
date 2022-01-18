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

package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"time"

	jsonpatch "github.com/evanphx/json-patch"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	tenancyv1alpha1 "github.com/kcp-dev/kcp/pkg/apis/tenancy/v1alpha1"
	kcpclient "github.com/kcp-dev/kcp/pkg/client/clientset/versioned"
	tenancyinformer "github.com/kcp-dev/kcp/pkg/client/informers/externalversions/tenancy/v1alpha1"
	tenancylister "github.com/kcp-dev/kcp/pkg/client/listers/tenancy/v1alpha1"
	conditionsv1alpha1 "github.com/kcp-dev/kcp/third_party/conditions/apis/conditions/v1alpha1"
	"github.com/kcp-dev/kcp/third_party/conditions/util/conditions"
)

const (
	currentShardIndex  = "shard"
	unschedulableIndex = "unschedulable"
	controllerName     = "workspace"
)

func NewController(
	kcpClient kcpclient.Scoper,
	workspaceInformer tenancyinformer.WorkspaceInformer,
	workspaceShardInformer tenancyinformer.WorkspaceShardInformer,
) (*Controller, error) {
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	c := &Controller{
		queue:                 queue,
		kcpClient:             kcpClient,
		workspaceIndexer:      workspaceInformer.Informer().GetIndexer(),
		workspaceLister:       workspaceInformer.Lister(),
		workspaceShardIndexer: workspaceShardInformer.Informer().GetIndexer(),
		workspaceShardLister:  workspaceShardInformer.Lister(),
		syncChecks: []cache.InformerSynced{
			workspaceInformer.Informer().HasSynced,
			workspaceShardInformer.Informer().HasSynced,
		},
	}

	workspaceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.enqueue(obj) },
		UpdateFunc: func(_, obj interface{}) { c.enqueue(obj) },
	})
	if err := c.workspaceIndexer.AddIndexers(map[string]cache.IndexFunc{
		currentShardIndex: func(obj interface{}) ([]string, error) {
			if workspace, ok := obj.(*tenancyv1alpha1.Workspace); ok {
				return []string{workspace.Status.Location.Current}, nil
			}
			return []string{}, nil
		},
		unschedulableIndex: func(obj interface{}) ([]string, error) {
			if workspace, ok := obj.(*tenancyv1alpha1.Workspace); ok {
				if conditions.IsFalse(workspace, tenancyv1alpha1.WorkspaceScheduled) && conditions.GetReason(workspace, tenancyv1alpha1.WorkspaceScheduled) == tenancyv1alpha1.WorkspaceReasonUnschedulable {
					return []string{"true"}, nil
				}
			}
			return []string{}, nil
		},
	}); err != nil {
		return nil, fmt.Errorf("failed to add indexer for Workspace: %w", err)
	}

	workspaceShardInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.enqueueAddedShard(obj) },
		DeleteFunc: func(obj interface{}) { c.enqueueDeletedShard(obj) },
	})

	return c, nil
}

// Controller watches Workspaces and WorkspaceShards in order to make sure every Workspace
// is scheduled to a valid WorkspaceShard.
type Controller struct {
	queue workqueue.RateLimitingInterface

	kcpClient        kcpclient.Scoper
	workspaceIndexer cache.Indexer
	workspaceLister  tenancylister.WorkspaceLister

	workspaceShardIndexer cache.Indexer
	workspaceShardLister  tenancylister.WorkspaceShardLister

	syncChecks []cache.InformerSynced
}

func (c *Controller) enqueue(obj interface{}) {
	key, err := cache.ObjectKeyFunc(obj)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	klog.Infof("queueing workspace %q", key)
	c.queue.Add(key)
}

func (c *Controller) enqueueAddedShard(obj interface{}) {
	shard, ok := obj.(*tenancyv1alpha1.WorkspaceShard)
	if !ok {
		runtime.HandleError(fmt.Errorf("got %T when handling added WorkspaceShard", obj))
		return
	}
	klog.Infof("handling added shard %q", shard.Name)
	workspaces, err := c.workspaceIndexer.ByIndex(unschedulableIndex, "true")
	if err != nil {
		runtime.HandleError(err)
		return
	}
	for _, workspace := range workspaces {
		key, err := cache.ObjectKeyFunc(workspace)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		klog.Infof("queuing unschedulable workspace %q", key)
		c.queue.Add(key)
	}
}

func (c *Controller) enqueueDeletedShard(obj interface{}) {
	shard, ok := obj.(*tenancyv1alpha1.WorkspaceShard)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.V(2).Infof("Couldn't get object from tombstone %#v", obj)
			return
		}
		shard, ok = tombstone.Obj.(*tenancyv1alpha1.WorkspaceShard)
		if !ok {
			klog.V(2).Infof("Tombstone contained object that is not a WorkspaceShard: %#v", obj)
			return
		}
	}
	klog.Infof("handling removed shard %q", shard.Name)
	workspaces, err := c.workspaceIndexer.ByIndex(currentShardIndex, shard.Name)
	if err != nil {
		runtime.HandleError(err)
		return
	}
	for _, workspace := range workspaces {
		key, err := cache.ObjectKeyFunc(workspace)
		if err != nil {
			runtime.HandleError(err)
			return
		}
		klog.Infof("queuing orphaned workspace %q", key)
		c.queue.Add(key)
	}
}

func (c *Controller) Start(ctx context.Context, numThreads int) {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Info("Starting Workspace controller")
	defer klog.Info("Shutting down Workspace controller")

	if !cache.WaitForNamedCacheSync(controllerName, ctx.Done(), c.syncChecks...) {
		klog.Warning("Failed to wait for caches to sync")
		return
	}

	for i := 0; i < numThreads; i++ {
		go wait.Until(func() { c.startWorker(ctx) }, time.Second, ctx.Done())
	}

	<-ctx.Done()
}

func (c *Controller) startWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

func (c *Controller) processNextWorkItem(ctx context.Context) bool {
	// Wait until there is a new item in the working queue
	k, quit := c.queue.Get()
	if quit {
		return false
	}
	key := k.(string)

	klog.Infof("processing key %q", key)

	// No matter what, tell the queue we're done with this key, to unblock
	// other workers.
	defer c.queue.Done(key)

	if err := c.process(ctx, key); err != nil {
		runtime.HandleError(fmt.Errorf("%q controller failed to sync %q, err: %w", controllerName, key, err))
		c.queue.AddRateLimited(key)
		return true
	}
	c.queue.Forget(key)
	return true
}

func (c *Controller) process(ctx context.Context, key string) error {
	scope, err := cache.ScopeFromKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error getting scope from key %q: %w", key, err))
		return nil
	}

	klog.Infof("ANDY scope %q listing workspace %q", scope.Name(), key)
	obj, err := c.workspaceLister.Scoped(scope).Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Infof("ANDY NOT FOUND")
			return nil // object deleted before we handled it
		}
		return err
	}
	previous := obj
	obj = obj.DeepCopy()

	if err := c.reconcile(ctx, scope, obj); err != nil {
		return err
	}

	// If the object being reconciled changed as a result, update it.
	if !equality.Semantic.DeepEqual(previous.Status, obj.Status) {
		oldData, err := json.Marshal(tenancyv1alpha1.Workspace{
			Status: previous.Status,
		})
		if err != nil {
			return fmt.Errorf("failed to Marshal old data for workspace %q: %w", key, err)
		}

		newData, err := json.Marshal(tenancyv1alpha1.Workspace{
			ObjectMeta: metav1.ObjectMeta{
				UID:             previous.UID,
				ResourceVersion: previous.ResourceVersion,
			}, // to ensure they appear in the patch as preconditions
			Status: obj.Status,
		})
		if err != nil {
			return fmt.Errorf("failed to Marshal new data for workspace %q: %w", key, err)
		}

		patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
		if err != nil {
			return fmt.Errorf("failed to create patch for workspace %q: %w", key, err)
		}
		klog.Infof("ANDY patching %q", string(patchBytes))
		_, uerr := c.kcpClient.Scope(scope).TenancyV1alpha1().Workspaces().Patch(ctx, obj.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
		klog.Infof("ANDY uerr=%v", uerr)
		return uerr
	} else {
		klog.Infof("ANDY NO CHANGES")
	}

	return nil
}

func (c *Controller) reconcile(ctx context.Context, scope rest.Scope, workspace *tenancyv1alpha1.Workspace) error {
	var shard *tenancyv1alpha1.WorkspaceShard
	if currentShardName := workspace.Status.Location.Current; currentShardName != "" {
		// make sure current shard still exists
		currentShard, err := c.workspaceShardLister.Scoped(scope).Get(currentShardName)
		if errors.IsNotFound(err) {
			klog.Infof("de-scheduling workspace %q from nonexistent shard %q", workspace.Name, currentShardName)
			workspace.Status.Location.Current = ""
		} else if err != nil {
			return err
		}
		shard = currentShard
	}
	if workspace.Status.Location.Current == "" {
		// find a shard for this workspace
		shards, err := c.workspaceShardLister.Scoped(scope).List(labels.Everything())
		if err != nil {
			return err
		}
		if len(shards) != 0 {
			targetShard := shards[rand.Intn(len(shards))]
			workspace.Status.Location.Target = targetShard.Name
			shard = targetShard
			klog.Infof("scheduling workspace %q to %q", workspace.Name, targetShard.Name)
		}
	}
	if workspace.Status.Location.Target != "" && workspace.Status.Location.Current != workspace.Status.Location.Target {
		klog.Infof("moving workspace %q from to %q", workspace.Name, workspace.Status.Location.Target)
		workspace.Status.Location.Current = workspace.Status.Location.Target
		workspace.Status.Location.Target = ""
	}
	if workspace.Status.Location.Current == "" {
		workspace.Status.Phase = tenancyv1alpha1.WorkspacePhaseInitializing
		conditions.MarkFalse(workspace, tenancyv1alpha1.WorkspaceScheduled, tenancyv1alpha1.WorkspaceReasonUnschedulable, conditionsv1alpha1.ConditionSeverityError, "No shards are available to schedule Workspaces to.")
	} else {
		workspace.Status.Phase = tenancyv1alpha1.WorkspacePhaseActive
		conditions.MarkTrue(workspace, tenancyv1alpha1.WorkspaceScheduled)
	}
	// expose the correct base URL given our current shard
	if shard == nil || !conditions.IsTrue(shard, tenancyv1alpha1.WorkspaceShardCredentialsValid) {
		conditions.MarkFalse(workspace, tenancyv1alpha1.WorkspaceURLValid, tenancyv1alpha1.WorkspaceURLReasonMissing, conditionsv1alpha1.ConditionSeverityError, "No connection information on target WorkspaceShard.")
	} else {
		u, err := url.Parse(shard.Status.ConnectionInfo.Host)
		if err != nil {
			conditions.MarkFalse(workspace, tenancyv1alpha1.WorkspaceURLValid, tenancyv1alpha1.WorkspaceURLReasonInvalid, conditionsv1alpha1.ConditionSeverityError, "Invalid connection information on target WorkspaceShard: %v.", err)
			return nil
		}
		u.Path = path.Join(u.Path, shard.Status.ConnectionInfo.APIPath, "clusters", workspace.Name)
		workspace.Status.BaseURL = u.String()
		conditions.MarkTrue(workspace, tenancyv1alpha1.WorkspaceURLValid)
	}
	return nil
}
