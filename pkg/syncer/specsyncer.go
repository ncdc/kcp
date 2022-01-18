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

package syncer

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kcp-dev/kcp/pkg/controllerz"
)

func deepEqualApartFromStatus(oldObj, newObj interface{}) bool {
	oldUnstrob, isOldObjUnstructured := oldObj.(*unstructured.Unstructured)
	newUnstrob, isNewObjUnstructured := newObj.(*unstructured.Unstructured)
	if !isOldObjUnstructured || !isNewObjUnstructured {
		return false
	}
	if !equality.Semantic.DeepEqual(oldUnstrob.GetAnnotations(), newUnstrob.GetAnnotations()) {
		return false
	}
	if !equality.Semantic.DeepEqual(oldUnstrob.GetLabels(), newUnstrob.GetLabels()) {
		return false
	}

	oldObjKeys := sets.StringKeySet(oldUnstrob.UnstructuredContent())
	newObjKeys := sets.StringKeySet(newUnstrob.UnstructuredContent())
	for _, key := range oldObjKeys.Union(newObjKeys).UnsortedList() {
		if key == "metadata" || key == "status" {
			continue
		}
		if !equality.Semantic.DeepEqual(oldUnstrob.UnstructuredContent()[key], newUnstrob.UnstructuredContent()[key]) {
			return false
		}
	}
	return true
}

const specSyncerAgent = "kcp#spec-syncer/v0.0.0"

func NewSpecSyncer(from, to *rest.Config, syncedResourceTypes []string, clusterID, logicalClusterID string) (*Controller, error) {
	from = rest.CopyConfig(from)
	from.UserAgent = specSyncerAgent
	to = rest.CopyConfig(to)
	to.UserAgent = specSyncerAgent

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(from)
	if err != nil {
		return nil, err
	}
	fromScope := controllerz.NewScope(logicalClusterID)
	fromDiscovery := discoveryClient.Scope(fromScope)
	fromClients, err := dynamic.NewScopingForConfig(from)
	if err != nil {
		return nil, err
	}
	fromClient := fromClients.Scope(fromScope)
	toClient := dynamic.NewForConfigOrDie(to)
	return New(fromDiscovery, fromClient, toClient, upsertIntoDownstream, deleteFromDownstream, func(c *Controller, gvr schema.GroupVersionResource) cache.ResourceEventHandlerFuncs {
		return cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) { c.AddToQueue(gvr, obj) },
			UpdateFunc: func(oldObj, newObj interface{}) {
				if !deepEqualApartFromStatus(oldObj, newObj) {
					c.AddToQueue(gvr, newObj)
				}
			},
			DeleteFunc: func(obj interface{}) { c.AddToQueue(gvr, obj) },
		}
	}, syncedResourceTypes, clusterID)
}

// TODO:
// This function is there as a quick and dirty implementation of namespace creation.
// In fact We should also be getting notifications about namespaces created upstream and be creating downstream equivalents.
func (c *Controller) ensureNamespaceExists(namespace string) error {
	namespaces := c.toClient.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "namespaces",
	})
	newNamespace := &unstructured.Unstructured{}
	newNamespace.SetAPIVersion("v1")
	newNamespace.SetKind("Namespace")
	newNamespace.SetName(namespace)
	if _, err := namespaces.Create(context.TODO(), newNamespace, metav1.CreateOptions{}); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			klog.Infof("Error while creating namespace %s: %v", namespace, err)
			return err
		}
	}
	return nil
}

func deleteFromDownstream(c *Controller, ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) error {
	// TODO: get UID of just-deleted object and pass it as a precondition on this delete.
	// This would avoid races where an object is deleted and another object with the same name is created immediately after.

	return c.getClient(gvr, namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func upsertIntoDownstream(c *Controller, ctx context.Context, gvr schema.GroupVersionResource, namespace string, unstrob *unstructured.Unstructured) error {
	if err := c.ensureNamespaceExists(namespace); err != nil {
		klog.Error(err)
		return err
	}

	client := c.getClient(gvr, namespace)

	unstrob = unstrob.DeepCopy()

	// Attempt to create the object; if the object already exists, update it.
	unstrob.SetUID("")
	unstrob.SetResourceVersion("")

	ownedByLabel := unstrob.GetLabels()["kcp.dev/owned-by"]
	var ownerReferences []metav1.OwnerReference
	for _, reference := range unstrob.GetOwnerReferences() {
		if reference.Name == ownedByLabel {
			continue
		}
		ownerReferences = append(ownerReferences, reference)
	}
	unstrob.SetOwnerReferences(ownerReferences)

	if _, err := client.Create(ctx, unstrob, metav1.CreateOptions{}); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			klog.Errorf("Creating resource %s/%s: %v", namespace, unstrob.GetName(), err)
			return err
		}

		existing, err := client.Get(ctx, unstrob.GetName(), metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Getting resource %s/%s: %v", namespace, unstrob.GetName(), err)
			return err
		}
		klog.Infof("Object %s/%s already exists: update it", gvr.Resource, unstrob.GetName())

		unstrob.SetResourceVersion(existing.GetResourceVersion())
		if _, err := client.Update(ctx, unstrob, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("Updating resource %s/%s: %v", namespace, unstrob.GetName(), err)
			return err
		}
		return nil
	}
	klog.Infof("Created object %s/%s", gvr.Resource, unstrob.GetName())
	return nil
}
