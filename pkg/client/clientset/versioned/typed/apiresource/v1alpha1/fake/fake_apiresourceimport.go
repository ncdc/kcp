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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"

	v1alpha1 "github.com/kcp-dev/kcp/pkg/apis/apiresource/v1alpha1"
)

// FakeAPIResourceImports implements APIResourceImportInterface
type FakeAPIResourceImports struct {
	Fake *FakeApiresourceV1alpha1
}

var apiresourceimportsResource = schema.GroupVersionResource{Group: "apiresource.kcp.dev", Version: "v1alpha1", Resource: "apiresourceimports"}

var apiresourceimportsKind = schema.GroupVersionKind{Group: "apiresource.kcp.dev", Version: "v1alpha1", Kind: "APIResourceImport"}

// Get takes name of the aPIResourceImport, and returns the corresponding aPIResourceImport object, and an error if there is any.
func (c *FakeAPIResourceImports) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.APIResourceImport, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(apiresourceimportsResource, name), &v1alpha1.APIResourceImport{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.APIResourceImport), err
}

// List takes label and field selectors, and returns the list of APIResourceImports that match those selectors.
func (c *FakeAPIResourceImports) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.APIResourceImportList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(apiresourceimportsResource, apiresourceimportsKind, opts), &v1alpha1.APIResourceImportList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.APIResourceImportList{ListMeta: obj.(*v1alpha1.APIResourceImportList).ListMeta}
	for _, item := range obj.(*v1alpha1.APIResourceImportList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested aPIResourceImports.
func (c *FakeAPIResourceImports) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(apiresourceimportsResource, opts))
}

// Create takes the representation of a aPIResourceImport and creates it.  Returns the server's representation of the aPIResourceImport, and an error, if there is any.
func (c *FakeAPIResourceImports) Create(ctx context.Context, aPIResourceImport *v1alpha1.APIResourceImport, opts v1.CreateOptions) (result *v1alpha1.APIResourceImport, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(apiresourceimportsResource, aPIResourceImport), &v1alpha1.APIResourceImport{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.APIResourceImport), err
}

// Update takes the representation of a aPIResourceImport and updates it. Returns the server's representation of the aPIResourceImport, and an error, if there is any.
func (c *FakeAPIResourceImports) Update(ctx context.Context, aPIResourceImport *v1alpha1.APIResourceImport, opts v1.UpdateOptions) (result *v1alpha1.APIResourceImport, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(apiresourceimportsResource, aPIResourceImport), &v1alpha1.APIResourceImport{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.APIResourceImport), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeAPIResourceImports) UpdateStatus(ctx context.Context, aPIResourceImport *v1alpha1.APIResourceImport, opts v1.UpdateOptions) (*v1alpha1.APIResourceImport, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(apiresourceimportsResource, "status", aPIResourceImport), &v1alpha1.APIResourceImport{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.APIResourceImport), err
}

// Delete takes name of the aPIResourceImport and deletes it. Returns an error if one occurs.
func (c *FakeAPIResourceImports) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteActionWithOptions(apiresourceimportsResource, name, opts), &v1alpha1.APIResourceImport{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeAPIResourceImports) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(apiresourceimportsResource, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.APIResourceImportList{})
	return err
}

// Patch applies the patch and returns the patched aPIResourceImport.
func (c *FakeAPIResourceImports) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.APIResourceImport, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(apiresourceimportsResource, name, pt, data, subresources...), &v1alpha1.APIResourceImport{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.APIResourceImport), err
}
