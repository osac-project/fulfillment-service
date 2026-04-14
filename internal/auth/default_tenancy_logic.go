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

	"github.com/osac-project/fulfillment-service/internal/collections"
)

// DefaultTenancyLogicBuilder contains the data and logic needed to create default tenancy logic.
type DefaultTenancyLogicBuilder struct {
	logger *slog.Logger
}

// DefaultTenancyLogic is the default implementation of TenancyLogic. It reads the tenants directly from the subject,
// which are expected to have been populated by the external authentication and authorization service.
type DefaultTenancyLogic struct {
	logger *slog.Logger
}

// NewDefaultTenancyLogic creates a new builder for default tenancy logic.
func NewDefaultTenancyLogic() *DefaultTenancyLogicBuilder {
	return &DefaultTenancyLogicBuilder{}
}

// SetLogger sets the logger that will be used by the tenancy logic.
func (b *DefaultTenancyLogicBuilder) SetLogger(value *slog.Logger) *DefaultTenancyLogicBuilder {
	b.logger = value
	return b
}

// Build creates the default tenancy logic that extracts the subject from the auth context and returns the identifiers
// of the tenants.
func (b *DefaultTenancyLogicBuilder) Build() (result *DefaultTenancyLogic, err error) {
	if b.logger == nil {
		err = fmt.Errorf("logger is mandatory")
		return
	}
	result = &DefaultTenancyLogic{
		logger: b.logger,
	}
	return
}

// DetermineAssignableTenants extracts the subject from the auth context and returns the identifiers of the tenants
// that can be assigned to objects.
func (p *DefaultTenancyLogic) DetermineAssignableTenants(ctx context.Context) (result collections.Set[string],
	err error) {
	subject := SubjectFromContext(ctx)
	result = subject.Tenants
	if result.Empty() {
		p.logger.ErrorContext(
			ctx,
			"Subject has no tenants",
			slog.String("user", subject.User),
		)
		err = fmt.Errorf("subject must belong to at least one tenant to create objects")
		return
	}
	return
}

// DetermineDefaultTenants extracts the subject from the auth context and returns the identifiers of the tenants
// that will be assigned by default to objects.
func (p *DefaultTenancyLogic) DetermineDefaultTenants(ctx context.Context) (result collections.Set[string],
	err error) {
	result, err = p.DetermineAssignableTenants(ctx)
	return
}

// DetermineVisibleTenants extracts the subject from the auth context and returns the identifiers of the tenants
// that the current user has permission to see, including the shared tenant.
func (p *DefaultTenancyLogic) DetermineVisibleTenants(ctx context.Context) (result collections.Set[string],
	err error) {
	subject := SubjectFromContext(ctx)
	result = subject.Tenants
	if result.Finite() {
		result = SharedTenants.Union(result)
	}
	return
}
