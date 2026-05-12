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
type Client interface {
	// Organization operations
	CreateOrganization(ctx context.Context, org *Organization) (*Organization, error)
	GetOrganization(ctx context.Context, name string) (*Organization, error)
	DeleteOrganization(ctx context.Context, name string) error

	// User operations
	CreateUser(ctx context.Context, organizationName string, user *User) (*User, error)
	GetUser(ctx context.Context, organizationName, userID string) (*User, error)
	ListUsers(ctx context.Context, organizationName string) ([]*User, error)
	DeleteUser(ctx context.Context, organizationName, userID string) error

	// Role operations
	// Roles can be at the organization level or client level
	ListOrganizationRoles(ctx context.Context, organizationName string) ([]*Role, error)
	ListClientRoles(ctx context.Context, organizationName, clientID string) ([]*Role, error)

	// User role assignments
	AssignOrganizationRolesToUser(ctx context.Context, organizationName, userID string, roles []*Role) error
	AssignClientRolesToUser(ctx context.Context, organizationName, userID, clientID string, roles []*Role) error
	RemoveOrganizationRolesFromUser(ctx context.Context, organizationName, userID string, roles []*Role) error
	RemoveClientRolesFromUser(ctx context.Context, organizationName, userID, clientID string, roles []*Role) error
	GetUserOrganizationRoles(ctx context.Context, organizationName, userID string) ([]*Role, error)
	GetUserClientRoles(ctx context.Context, organizationName, userID, clientID string) ([]*Role, error)

	// Admin permissions
	// AssignOrganizationAdminPermissions grants full administrative access to an organization for the specified user.
	// The implementation is provider-specific:
	// - Keycloak: Assigns tenant-admin role
	// - Auth0: Assigns organization Admin role
	// - Okta: Assigns Organizational Administrator role
	// - Azure AD: Assigns Global Administrator or Organizational Administrator role
	AssignOrganizationAdminPermissions(ctx context.Context, organizationName, userID string) error

	// AssignIdpManagerPermissions grants limited IdP management permissions to the specified user.
	// This is used for the break-glass account which can manage user roles and identity providers
	// but cannot modify critical organization settings.
	// The implementation is provider-specific:
	// - Keycloak: Assigns limited tenant-idp-manager role
	// - Auth0: Assigns organization Member Manager role
	// - Okta: Assigns User Administrator role
	// - Azure AD: Assigns User Administrator role
	AssignIdpManagerPermissions(ctx context.Context, userID string) error
}
