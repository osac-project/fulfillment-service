/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package idp

import (
	"context"
)

//go:generate go run go.uber.org/mock/mockgen -destination=client_mock.go -package=idp . Client

// Client is the generic interface for identity provider admin operations.
// Different IdP providers (Keycloak, Auth0, Okta, etc.) implement this interface.
//
// For Keycloak:
// - One realm contains all OSAC (e.g., "osac" realm)
// - Tenants map to Keycloak Organizations within that realm
// - Identity providers are realm-level resources assigned to tenants
type Client interface {
	// Tenant operations
	// These manage tenants in the identity provider (e.g., Keycloak Organizations).
	CreateTenant(ctx context.Context, tenant *Tenant) (*Tenant, error)
	GetTenant(ctx context.Context, name string) (*Tenant, error)
	UpdateTenant(ctx context.Context, tenant *Tenant) (*Tenant, error)
	DeleteTenant(ctx context.Context, tenantName string) error

	// User operations
	// All user operations accept idpUserID, which is the identity provider's user UUID stored in User.status.keycloak_user_id.
	// Controllers should fetch the OSAC User object and extract status.keycloak_user_id before calling these methods.
	CreateUser(ctx context.Context, tenantName string, user *User) (*User, error)
	GetUser(ctx context.Context, tenantName, idpUserID string) (*User, error)
	ListUsers(ctx context.Context, tenantName string) ([]*User, error)
	DeleteUser(ctx context.Context, tenantName, idpUserID string) error

	// Role operations
	// Roles can be at the tenant level or client level
	ListTenantRoles(ctx context.Context, tenantName string) ([]*Role, error)
	ListClientRoles(ctx context.Context, tenantName, clientID string) ([]*Role, error)

	// User role assignments
	// All role assignment methods accept idpUserID (the identity provider's user UUID, not the OSAC user ID).
	// Controllers must:
	// 1. Fetch the OSAC User object by the user ID/name from the API request
	// 2. Extract idpUserID from user.status.keycloak_user_id
	// 3. Pass idpUserID to these IDP client methods
	AssignTenantRolesToUser(ctx context.Context, tenantName, idpUserID string, roles []*Role) error
	AssignClientRolesToUser(ctx context.Context, tenantName, idpUserID, clientID string, roles []*Role) error
	RemoveTenantRolesFromUser(ctx context.Context, tenantName, idpUserID string, roles []*Role) error
	RemoveClientRolesFromUser(ctx context.Context, tenantName, idpUserID, clientID string, roles []*Role) error
	GetUserTenantRoles(ctx context.Context, tenantName, idpUserID string) ([]*Role, error)
	GetUserClientRoles(ctx context.Context, tenantName, idpUserID, clientID string) ([]*Role, error)

	// Admin permissions
	// AssignTenantAdminPermissions grants full administrative access to a tenant for the specified user.
	// The implementation is provider-specific:
	// - Keycloak: Assigns tenant-admin role
	// - Auth0: Assigns organization Admin role
	// - Okta: Assigns Organizational Administrator role
	// - Azure AD: Assigns Global Administrator or Organizational Administrator role
	AssignTenantAdminPermissions(ctx context.Context, tenantName, idpUserID string) error

	// AssignIdpManagerPermissions grants limited IdP management permissions to the specified user.
	// This is used for the break-glass account which can manage user roles and identity providers
	// but cannot modify critical tenant settings.
	// The implementation is provider-specific:
	// - Keycloak: Assigns limited tenant-idp-manager role
	// - Auth0: Assigns organization Member Manager role
	// - Okta: Assigns User Administrator role
	// - Azure AD: Assigns User Administrator role
	AssignIdpManagerPermissions(ctx context.Context, idpUserID string) error

	// Authorization resource operations (Keycloak Authorization Services / UMA 2.0)
	// These methods manage fine-grained permissions on resources like Projects.
	// Note: Not all IdPs support this - it's primarily a Keycloak feature.
	CreateAuthorizationResource(ctx context.Context, resource *AuthorizationResource) (*AuthorizationResource, error)
	GetAuthorizationResource(ctx context.Context, resourceID string) (*AuthorizationResource, error)
	DeleteAuthorizationResource(ctx context.Context, resourceID string) error

	// Authorization policy and permission operations
	// These methods control who can access which resources with what scopes.
	// CreateAuthorizationGroup creates a tenant group for authorization purposes.
	// Groups are scoped to a specific tenant and support hierarchical paths.
	// Recommended path format: "/{project-name}/{system:viewers|system:managers}" for top-level projects.
	// Returns the created group ID.
	CreateAuthorizationGroup(ctx context.Context, tenantName, groupPath string) (string, error)
	// DeleteAuthorizationGroup deletes a tenant group by ID.
	DeleteAuthorizationGroup(ctx context.Context, tenantName, groupID string) error
	// GetGroupIDByPath gets a tenant group ID by its path.
	GetGroupIDByPath(ctx context.Context, tenantName, groupPath string) (string, error)
	// AddUserToGroup adds a user to a tenant group by group ID.
	AddUserToGroup(ctx context.Context, tenantName, username, groupID string) error
	// RemoveUserFromGroup removes a user from a tenant group by group ID.
	RemoveUserFromGroup(ctx context.Context, tenantName, username, groupID string) error

	// Identity Provider operations
	// CreateIdentityProvider creates a new external identity provider for a specific tenant.
	CreateIdentityProvider(ctx context.Context, tenantName string, idp *IdentityProvider) (*IdentityProvider, error)

	// GetIdentityProvider retrieves an identity provider by alias for a specific tenant.
	GetIdentityProvider(ctx context.Context, tenantName, alias string) (*IdentityProvider, error)

	// ListIdentityProviders lists all identity providers for a specific tenant.
	// Returns an empty slice if no IdPs are configured for the tenant.
	ListIdentityProviders(ctx context.Context, tenantName string) ([]*IdentityProvider, error)

	// DeleteIdentityProvider deletes an identity provider for a specific tenant.
	DeleteIdentityProvider(ctx context.Context, tenantName, alias string) error
}
