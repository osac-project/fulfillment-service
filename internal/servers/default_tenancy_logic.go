/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
	"github.com/osac-project/fulfillment-service/internal/database"
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
	subject := auth.SubjectFromContext(ctx)
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

// DetermineDefaultTenant extracts the subject from the auth context and returns the tenant that will be assigned
// by default to objects. When the subject has access to all tenants (e.g. an admin), the default is the shared
// tenant because a universal set can't be stored as the tenant of an object.
func (p *DefaultTenancyLogic) DetermineDefaultTenant(ctx context.Context) (result string, err error) {
	assignable, err := p.DetermineAssignableTenants(ctx)
	if err != nil {
		return
	}
	if !assignable.Finite() {
		result = auth.SharedTenant
		return
	}
	inclusions := assignable.Inclusions()
	if len(inclusions) > 0 {
		result = inclusions[0]
	}
	return
}

// DetermineVisibility extracts the subject from the auth context and returns the set of project names that the
// current user has permission to see for each visible tenant. The returned visibility is frozen and must not be
// modified.
func (p *DefaultTenancyLogic) DetermineVisibility(ctx context.Context) (result *auth.Visibility, err error) {
	subject := auth.SubjectFromContext(ctx)

	// If the subject has access to all tenants (e.g. an admin), return a total visibility that grants access
	// to everything:
	if !subject.Tenants.Finite() {
		result = auth.TotalVisibility
		return
	}

	// Start by adding the shared tenant:
	builder := auth.NewVisibility()

	// TODO: Currently the system is not prepared for the restrictive visibility model where users need to be
	// explicitly added to projects. This will be fixed in the future, but for now we will add the default
	// project of the tenant so that users will have the same visibility as before.
	builder.AddProject(auth.SharedTenant, auth.DefaultProject)
	for _, tenant := range subject.Tenants.Inclusions() {
		builder.AddProject(tenant, auth.DefaultProject)
	}

	// Add the projects that the user has access to according to the project membership table:
	tx, err := database.TxFromContext(ctx)
	if err != nil {
		return
	}
	rows, err := tx.Query(
		ctx,
		`select tenant, project from project_membership_subjects where "user" = $1`,
		subject.User,
	)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var tenant, project string
		err = rows.Scan(&tenant, &project)
		if err != nil {
			return
		}
		builder.AddProject(tenant, project)
	}
	err = rows.Err()
	if err != nil {
		return
	}

	// Build and return the visibility:
	result, err = builder.Build()
	return
}
