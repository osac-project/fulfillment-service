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

// JwtTenancyLogicBuilder contains the data and logic needed to create JWT tenancy logic.
type JwtTenancyLogicBuilder struct {
	logger *slog.Logger
}

// JwtTenancyLogic implements the TenancyLogic interface for regular users authenticated with JSON web tokens. It
// extracts the tenants from the user and groups that were extracted from the claims of the token.
type JwtTenancyLogic struct {
	logger *slog.Logger
}

// NewJwtTenancyLogic creates a new builder for JSON web token tenancy logic.
func NewJwtTenancyLogic() *JwtTenancyLogicBuilder {
	return &JwtTenancyLogicBuilder{}
}

// SetLogger sets the logger that will be used by the tenancy logic. This is mandatory.
func (b *JwtTenancyLogicBuilder) SetLogger(value *slog.Logger) *JwtTenancyLogicBuilder {
	b.logger = value
	return b
}

// Build uses the information stored in the builder to create a new instance of the tenancy logic.
func (b *JwtTenancyLogicBuilder) Build() (result *JwtTenancyLogic, err error) {
	// Check that the logger has been set:
	if b.logger == nil {
		err = fmt.Errorf("logger is mandatory")
		return
	}

	// Create the tenancy logic:
	result = &JwtTenancyLogic{
		logger: b.logger,
	}
	return
}

// DetermineAssignableTenants extracts the subject from the auth context and returns the identifiers of the tenants
// that can be assigned to objects.
func (p *JwtTenancyLogic) DetermineAssignableTenants(ctx context.Context) (result collections.Set[string], err error) {
	subject := SubjectFromContext(ctx)
	result = collections.NewSet(subject.Groups...)
	if len(subject.Groups) == 0 {
		p.logger.ErrorContext(
			ctx,
			"JWT user has no groups",
			slog.String("user", subject.User),
		)
		err = fmt.Errorf("user must belong to at least one group to create objects")
		return
	}
	return
}

// DetermineDefaultTenants extracts the subject from the auth context and returns the identifiers of the tenants.
// For JWT-authenticated users, objects are assigned to the groups of the user.
func (p *JwtTenancyLogic) DetermineDefaultTenants(ctx context.Context) (result collections.Set[string], err error) {
	result, err = p.DetermineAssignableTenants(ctx)
	return
}

// DetermineVisibleTenants extracts the subject from the context and returns a tenant for each group that the user
// belongs to, as well as the shared tenant.
func (p *JwtTenancyLogic) DetermineVisibleTenants(ctx context.Context) (result collections.Set[string], err error) {
	subject := SubjectFromContext(ctx)
	result = SharedTenants.Union(collections.NewSet(subject.Groups...))
	return
}
