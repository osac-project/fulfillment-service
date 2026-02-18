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
	"log/slog"

	"github.com/osac-project/fulfillment-service/internal/collections"
)

// SystemTenancyLogicBuilder contains the data and logic needed to create system tenancy logic.
type SystemTenancyLogicBuilder struct {
	logger *slog.Logger
}

// SystemTenancyLogic is a tenancy logic implementation intended exclusively for the private API, where tenancy
// filtering is effectively disabled. It assigns objects to the shared tenant while returning an empty set of
// visible tenants, which allows the private API to access all objects regardless of their tenant assignment.
// This implementation should NOT be used for the public API.
type SystemTenancyLogic struct {
	logger *slog.Logger
}

// NewSystemTenancyLogic creates a new builder for system tenancy logic.
func NewSystemTenancyLogic() *SystemTenancyLogicBuilder {
	return &SystemTenancyLogicBuilder{}
}

// SetLogger sets the logger that will be used by the tenancy logic.
func (b *SystemTenancyLogicBuilder) SetLogger(value *slog.Logger) *SystemTenancyLogicBuilder {
	b.logger = value
	return b
}

// Build creates the system tenancy logic.
func (b *SystemTenancyLogicBuilder) Build() (result *SystemTenancyLogic, err error) {
	// Create the tenancy logic:
	result = &SystemTenancyLogic{
		logger: b.logger,
	}
	return
}

// DetermineAssignableTenants returns a universal set of tenants, which allows the private API to assign objects to any
// tenant.
func (p *SystemTenancyLogic) DetermineAssignableTenants(_ context.Context) (result collections.Set[string], err error) {
	result = AllTenants
	return
}

// DetermineDefaultTenants returns the shared tenant for objects created through the private API.
func (p *SystemTenancyLogic) DetermineDefaultTenants(_ context.Context) (result collections.Set[string], err error) {
	result = SharedTenants
	return
}

// DetermineVisibleTenants returns a universal set of tenants, which disables tenant filtering and allows the private
// API to access all objects regardless of their tenant assignment.
func (p *SystemTenancyLogic) DetermineVisibleTenants(_ context.Context) (result collections.Set[string], err error) {
	result = AllTenants
	return
}
