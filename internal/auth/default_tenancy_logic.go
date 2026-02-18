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

// DefaultTenancyLogic is the default implementation of TenancyLogic that supports both service accounts and regular
// users authenticated with JWT. It checks the user name prefix to determine the type and delegates to the appropriate
// logic.
type DefaultTenancyLogic struct {
	logger      *slog.Logger
	saDelegate  TenancyLogic
	jwtDelegate TenancyLogic
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
	// Check that the logger has been set:
	if b.logger == nil {
		err = fmt.Errorf("logger is mandatory")
		return
	}

	// Create the delegates:
	saDelegate, err := NewServiceAccountTenancyLogic().
		SetLogger(b.logger).
		Build()
	if err != nil {
		return
	}
	jwtDelegate, err := NewJwtTenancyLogic().
		SetLogger(b.logger).
		Build()
	if err != nil {
		return
	}

	// Create the tenancy logic:
	result = &DefaultTenancyLogic{
		logger:      b.logger,
		saDelegate:  saDelegate,
		jwtDelegate: jwtDelegate,
	}
	return
}

// DetermineAssignableTenants extracts the subject from the auth context and returns the identifiers of the tenants
// that can be assigned to objects.
func (p *DefaultTenancyLogic) DetermineAssignableTenants(ctx context.Context) (result collections.Set[string], err error) {
	subject := SubjectFromContext(ctx)
	delegate, err := p.selectDelegate(subject)
	if err != nil {
		return
	}
	return delegate.DetermineAssignableTenants(ctx)
}

// DetermineDefaultTenants extracts the subject from the auth context and returns the identifiers of the tenants
// that will be assigned by default to objects.
func (p *DefaultTenancyLogic) DetermineDefaultTenants(ctx context.Context) (result collections.Set[string], err error) {
	subject := SubjectFromContext(ctx)
	delegate, err := p.selectDelegate(subject)
	if err != nil {
		return
	}
	return delegate.DetermineDefaultTenants(ctx)
}

// DetermineVisibleTenants extracts the subject from the auth context and returns the identifiers of the tenants
// that the current user has permission to see.
func (p *DefaultTenancyLogic) DetermineVisibleTenants(ctx context.Context) (result collections.Set[string], err error) {
	subject := SubjectFromContext(ctx)
	delegate, err := p.selectDelegate(subject)
	if err != nil {
		return
	}
	return delegate.DetermineVisibleTenants(ctx)
}

// selectDelegate selects the appropriate tenancy logic delegate based on the subject source.
func (p *DefaultTenancyLogic) selectDelegate(subject *Subject) (result TenancyLogic, err error) {
	switch subject.Source {
	case SubjectSourceServiceAccount:
		result = p.saDelegate
	case SubjectSourceJwt, SubjectSourceNone:
		result = p.jwtDelegate
	default:
		err = fmt.Errorf("unknown subject source '%s'", subject.Source)
	}
	return
}
