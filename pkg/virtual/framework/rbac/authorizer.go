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

package rbac

import (
	rbacinformers "k8s.io/client-go/informers/rbac/v1"
	"k8s.io/client-go/rest"
	rbacregistryvalidation "k8s.io/kubernetes/pkg/registry/rbac/validation"
	rbacauthorizer "k8s.io/kubernetes/plugin/pkg/auth/authorizer/rbac"
)

func NewRuleResolver(informers rbacinformers.Interface, scope rest.Scope) rbacregistryvalidation.AuthorizationRuleResolver {
	return rbacregistryvalidation.NewDefaultRuleResolver(
		&rbacauthorizer.RoleGetter{Lister: informers.Roles().Lister().Scoped(scope)},
		&rbacauthorizer.RoleBindingLister{Lister: informers.RoleBindings().Lister().Scoped(scope)},
		&rbacauthorizer.ClusterRoleGetter{Lister: informers.ClusterRoles().Lister().Scoped(scope)},
		&rbacauthorizer.ClusterRoleBindingLister{Lister: informers.ClusterRoleBindings().Lister().Scoped(scope)},
	)
}

func NewSubjectLocator(informers rbacinformers.Interface, scope rest.Scope) rbacauthorizer.SubjectLocator {
	return rbacauthorizer.NewSubjectAccessEvaluator(
		&rbacauthorizer.RoleGetter{Lister: informers.Roles().Lister().Scoped(scope)},
		&rbacauthorizer.RoleBindingLister{Lister: informers.RoleBindings().Lister().Scoped(scope)},
		&rbacauthorizer.ClusterRoleGetter{Lister: informers.ClusterRoles().Lister().Scoped(scope)},
		&rbacauthorizer.ClusterRoleBindingLister{Lister: informers.ClusterRoleBindings().Lister().Scoped(scope)},
		"",
	)
}
