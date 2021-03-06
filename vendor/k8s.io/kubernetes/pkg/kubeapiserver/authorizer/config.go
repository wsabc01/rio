/*
Copyright 2016 The Kubernetes Authors.

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

package authorizer

import (
	"errors"
	"fmt"

	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/authorizerfactory"
	"k8s.io/apiserver/pkg/authorization/union"
	versionedinformers "k8s.io/client-go/informers"
	"k8s.io/kubernetes/pkg/auth/nodeidentifier"
	informers "k8s.io/kubernetes/pkg/client/informers/informers_generated/internalversion"
	"k8s.io/kubernetes/pkg/kubeapiserver/authorizer/modes"
	"k8s.io/kubernetes/plugin/pkg/auth/authorizer/node"
	"k8s.io/kubernetes/plugin/pkg/auth/authorizer/rbac"
	"k8s.io/kubernetes/plugin/pkg/auth/authorizer/rbac/bootstrappolicy"
)

type AuthorizationConfig struct {
	AuthorizationModes []string

	InformerFactory          informers.SharedInformerFactory
	VersionedInformerFactory versionedinformers.SharedInformerFactory
}

// New returns the right sort of union of multiple authorizer.Authorizer objects
// based on the authorizationMode or an error.
func (config AuthorizationConfig) New() (authorizer.Authorizer, authorizer.RuleResolver, error) {
	if len(config.AuthorizationModes) == 0 {
		return nil, nil, errors.New("At least one authorization mode should be passed")
	}

	var (
		authorizers   []authorizer.Authorizer
		ruleResolvers []authorizer.RuleResolver
	)
	authorizerMap := make(map[string]bool)

	for _, authorizationMode := range config.AuthorizationModes {
		if authorizerMap[authorizationMode] {
			return nil, nil, fmt.Errorf("Authorization mode %s specified more than once", authorizationMode)
		}

		// Keep cases in sync with constant list above.
		switch authorizationMode {
		case modes.ModeNode:
			graph := node.NewGraph()
			node.AddGraphEventHandlers(
				graph,
				config.InformerFactory.Core().InternalVersion().Pods(),
				config.InformerFactory.Core().InternalVersion().PersistentVolumes(),
				config.VersionedInformerFactory.Storage().V1beta1().VolumeAttachments(),
			)
			nodeAuthorizer := node.NewAuthorizer(graph, nodeidentifier.NewDefaultNodeIdentifier(), bootstrappolicy.NodeRules())
			authorizers = append(authorizers, nodeAuthorizer)

		case modes.ModeAlwaysAllow:
			alwaysAllowAuthorizer := authorizerfactory.NewAlwaysAllowAuthorizer()
			authorizers = append(authorizers, alwaysAllowAuthorizer)
			ruleResolvers = append(ruleResolvers, alwaysAllowAuthorizer)
		case modes.ModeAlwaysDeny:
			alwaysDenyAuthorizer := authorizerfactory.NewAlwaysDenyAuthorizer()
			authorizers = append(authorizers, alwaysDenyAuthorizer)
			ruleResolvers = append(ruleResolvers, alwaysDenyAuthorizer)
		case modes.ModeRBAC:
			rbacAuthorizer := rbac.New(
				&rbac.RoleGetter{Lister: config.InformerFactory.Rbac().InternalVersion().Roles().Lister()},
				&rbac.RoleBindingLister{Lister: config.InformerFactory.Rbac().InternalVersion().RoleBindings().Lister()},
				&rbac.ClusterRoleGetter{Lister: config.InformerFactory.Rbac().InternalVersion().ClusterRoles().Lister()},
				&rbac.ClusterRoleBindingLister{Lister: config.InformerFactory.Rbac().InternalVersion().ClusterRoleBindings().Lister()},
			)
			authorizers = append(authorizers, rbacAuthorizer)
			ruleResolvers = append(ruleResolvers, rbacAuthorizer)
		default:
			return nil, nil, fmt.Errorf("Unknown authorization mode %s specified", authorizationMode)
		}
		authorizerMap[authorizationMode] = true
	}

	return union.New(authorizers...), union.NewRuleResolvers(ruleResolvers...), nil
}
