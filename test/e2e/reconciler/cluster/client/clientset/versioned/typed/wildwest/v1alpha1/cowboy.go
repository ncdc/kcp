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

package v1alpha1

import (
	"context"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"

	v1alpha1 "github.com/kcp-dev/kcp/test/e2e/reconciler/cluster/apis/wildwest/v1alpha1"
	scheme "github.com/kcp-dev/kcp/test/e2e/reconciler/cluster/client/clientset/versioned/scheme"
)

// CowboysGetter has a method to return a CowboyInterface.
// A group's client should implement this interface.
type CowboysGetter interface {
	Cowboys(namespace string) CowboyInterface
}

type ScopedCowboysGetter interface {
	ScopedCowboys(scope rest.Scope, namespace string) CowboyInterface
}

// CowboyInterface has methods to work with Cowboy resources.
type CowboyInterface interface {
	Create(ctx context.Context, cowboy *v1alpha1.Cowboy, opts v1.CreateOptions) (*v1alpha1.Cowboy, error)
	Update(ctx context.Context, cowboy *v1alpha1.Cowboy, opts v1.UpdateOptions) (*v1alpha1.Cowboy, error)
	UpdateStatus(ctx context.Context, cowboy *v1alpha1.Cowboy, opts v1.UpdateOptions) (*v1alpha1.Cowboy, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.Cowboy, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.CowboyList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.Cowboy, err error)
	CowboyExpansion
}

// cowboys implements CowboyInterface
type cowboys struct {
	client  rest.Interface
	cluster string
	scope   rest.Scope
	ns      string
}

// newCowboys returns a Cowboys
func newCowboys(c *WildwestV1alpha1Client, scope rest.Scope, namespace string) *cowboys {
	return &cowboys{
		client:  c.RESTClient(),
		cluster: c.cluster,
		scope:   scope,
		ns:      namespace,
	}
}

// Get takes name of the cowboy, and returns the corresponding cowboy object, and an error if there is any.
func (c *cowboys) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.Cowboy, err error) {
	result = &v1alpha1.Cowboy{}
	err = c.client.Get().
		Cluster(c.cluster).
		Scope(c.scope).
		Namespace(c.ns).
		Resource("cowboys").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of Cowboys that match those selectors.
func (c *cowboys) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.CowboyList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.CowboyList{}
	err = c.client.Get().
		Cluster(c.cluster).
		Scope(c.scope).
		Namespace(c.ns).
		Resource("cowboys").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested cowboys.
func (c *cowboys) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Cluster(c.cluster).
		Scope(c.scope).
		Namespace(c.ns).
		Resource("cowboys").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a cowboy and creates it.  Returns the server's representation of the cowboy, and an error, if there is any.
func (c *cowboys) Create(ctx context.Context, cowboy *v1alpha1.Cowboy, opts v1.CreateOptions) (result *v1alpha1.Cowboy, err error) {
	result = &v1alpha1.Cowboy{}
	err = c.client.Post().
		Cluster(c.cluster).
		Scope(c.scope).
		Namespace(c.ns).
		Resource("cowboys").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(cowboy).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a cowboy and updates it. Returns the server's representation of the cowboy, and an error, if there is any.
func (c *cowboys) Update(ctx context.Context, cowboy *v1alpha1.Cowboy, opts v1.UpdateOptions) (result *v1alpha1.Cowboy, err error) {
	result = &v1alpha1.Cowboy{}
	err = c.client.Put().
		Cluster(c.cluster).
		Scope(c.scope).
		Namespace(c.ns).
		Resource("cowboys").
		Name(cowboy.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(cowboy).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *cowboys) UpdateStatus(ctx context.Context, cowboy *v1alpha1.Cowboy, opts v1.UpdateOptions) (result *v1alpha1.Cowboy, err error) {
	result = &v1alpha1.Cowboy{}
	err = c.client.Put().
		Cluster(c.cluster).
		Scope(c.scope).
		Namespace(c.ns).
		Resource("cowboys").
		Name(cowboy.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(cowboy).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the cowboy and deletes it. Returns an error if one occurs.
func (c *cowboys) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Cluster(c.cluster).
		Scope(c.scope).
		Namespace(c.ns).
		Resource("cowboys").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *cowboys) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Cluster(c.cluster).
		Scope(c.scope).
		Namespace(c.ns).
		Resource("cowboys").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched cowboy.
func (c *cowboys) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.Cowboy, err error) {
	result = &v1alpha1.Cowboy{}
	err = c.client.Patch(pt).
		Cluster(c.cluster).
		Scope(c.scope).
		Namespace(c.ns).
		Resource("cowboys").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
