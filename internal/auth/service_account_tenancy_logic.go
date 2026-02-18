/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package auth

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/osac-project/fulfillment-service/internal/collections"
)

// ServiceAccountTenancyLogicBuilder contains the data and logic needed to create default tenancy logic.
type ServiceAccountTenancyLogicBuilder struct {
	logger *slog.Logger
}

// ServiceAccountTenancyLogic implements the tenancy TenancyLogic interface assuming that the subject is a Kubernetes
// service account authenticated using the Kubernetes token review API. It uses the namespace of the service account
// as the tenant identifier.
type ServiceAccountTenancyLogic struct {
	logger *slog.Logger
}

// NewServiceAccountTenancyLogic creates a new builder for default tenancy logic.
func NewServiceAccountTenancyLogic() *ServiceAccountTenancyLogicBuilder {
	return &ServiceAccountTenancyLogicBuilder{}
}

// SetLogger sets the logger that will be used by the tenancy logic.
func (b *ServiceAccountTenancyLogicBuilder) SetLogger(value *slog.Logger) *ServiceAccountTenancyLogicBuilder {
	b.logger = value
	return b
}

// Build creates the default tenancy logic that extracts the subject from the auth context and returns the identifiers
// of the tenants.
func (b *ServiceAccountTenancyLogicBuilder) Build() (result *ServiceAccountTenancyLogic, err error) {
	// Check that the logger has been set:
	if b.logger == nil {
		err = fmt.Errorf("logger is mandatory")
		return
	}

	// Create the tenancy logic:
	result = &ServiceAccountTenancyLogic{
		logger: b.logger,
	}
	return
}

// DetermineAssignableTenants extracts the subject from the auth context and returns the identifiers of the tenants
// that can be assigned to objects.
func (p *ServiceAccountTenancyLogic) DetermineAssignableTenants(ctx context.Context) (result collections.Set[string],
	err error) {
	// Objects created by a service account are assigned to a tenant that is the namespace of the service account.
	namespace, err := p.extractNamespace(ctx)
	if err != nil {
		return
	}
	result = collections.NewSet(namespace)
	return
}

// DetermineDefaultTenants extracts the subject from the auth context and returns the identifiers of the tenants
// that will be assigned by default to objects.
func (p *ServiceAccountTenancyLogic) DetermineDefaultTenants(ctx context.Context) (result collections.Set[string],
	err error) {
	result, err = p.DetermineAssignableTenants(ctx)
	return
}

// DetermineVisibleTenants extracts the subject from the auth context and returns the identifiers of the tenants
// that the current user has permission to see.
func (p *ServiceAccountTenancyLogic) DetermineVisibleTenants(ctx context.Context) (result collections.Set[string],
	err error) {
	// A service account can see the resources correponding to the tenant with the same name as the namespace of
	// the service account, as well as the shared tenant.
	namespace, err := p.extractNamespace(ctx)
	if err != nil {
		return
	}
	result = SharedTenants.Union(collections.NewSet(namespace))
	return
}

// extractNamespace extracts the namespace of the service account from the subject.
func (p *ServiceAccountTenancyLogic) extractNamespace(ctx context.Context) (result string, err error) {
	// When authorino checks the authentication of a service account using the Kubernetes token review API, it adds
	// a something like 'system:serviceaccount:my-ns:my-sa' to the 'input.auth.identity.user.username' field of the
	// auth JSON, and then our configuration puts that into the 'user' field of the 'x-subject' header. Here we
	// extract the namespace name.
	subject := SubjectFromContext(ctx)
	matches := serviceAccountTenancyLogicNamespaceRegex.FindStringSubmatch(subject.User)
	if matches == nil {
		err = fmt.Errorf(
			"subject '%s' is not a service account, it should match regular expression '%s'",
			subject.User, serviceAccountTenancyLogicNamespaceRegex.String(),
		)
		return
	}
	result = matches[1]
	p.logger.DebugContext(
		ctx,
		"Extracted namespace",
		slog.String("user", subject.User),
		slog.String("namespace", result),
	)
	return
}

// serviceAccountTenancyLogicNamespaceRegex is a regular expression that matches and extracts the namespace of a
// service account from the subject user name.
var serviceAccountTenancyLogicNamespaceRegex = regexp.MustCompile(`^system:serviceaccount:([^:]*):[^:]*$`)
