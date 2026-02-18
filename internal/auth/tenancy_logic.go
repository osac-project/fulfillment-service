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

	"github.com/osac-project/fulfillment-service/internal/collections"
)

// TenancyLogic defines the logic for determining object tenancy and access control.
//
//go:generate mockgen -destination=tenancy_logic_mock.go -package=auth . TenancyLogic
type TenancyLogic interface {
	// DetermineAssignableTenants calculates and returns the list of tenant names that can be assigned to an object
	// that is being created or updated. This should be a superset of the default tenants.
	DetermineAssignableTenants(ctx context.Context) (collections.Set[string], error)

	// DetermineDefaultTenants calculates and returns the list of tenant names that are assigned by default when an
	// object is created without an explicit tenant request. This should be a subset of the assignable tenants.
	DetermineDefaultTenants(ctx context.Context) (collections.Set[string], error)

	// DetermineVisibleTenants calculates and returns the list of tenant names that the current user has permission
	// to see. Database queries will be filtered to only return objects where the tenants column has a non-empty
	// intersection with the values returned by this method.
	DetermineVisibleTenants(ctx context.Context) (collections.Set[string], error)
}

// SystemTenants is the set of tenants that are assigned to objects that are only visible to the system.
var SystemTenants = collections.NewSet("system")

// SharedTenants is the set of tenants that are always visible to all users.
var SharedTenants = collections.NewSet("shared")

// AllTenants is the set of all tenants that are possible.
var AllTenants = collections.NewUniversal[string]()
