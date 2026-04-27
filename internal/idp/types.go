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

// ResourceType represents the type of IDP resource.
type ResourceType string

const (
	// ResourceTypeOrganization represents an organization/realm/tenant in the IDP.
	ResourceTypeOrganization ResourceType = "organization"

	// ResourceTypeUser represents a user account in the IDP.
	ResourceTypeUser ResourceType = "user"

	// ResourceTypeRole represents a role in the IDP.
	ResourceTypeRole ResourceType = "role"
)

// Organization represents a logical grouping of users, groups, and applications in an IdP.
// Different providers call this different things:
// - Keycloak: Realm
// - Auth0: Tenant
// - Okta: Organization
// - Azure AD: Tenant
type Organization struct {
	ID          string
	Name        string
	DisplayName string
	Enabled     bool
	Attributes  map[string][]string
}

// User represents a user in the identity provider.
type User struct {
	ID              string
	Username        string
	Email           string
	EmailVerified   bool
	Enabled         bool
	FirstName       string
	LastName        string
	Attributes      map[string][]string
	Groups          []string
	Credentials     []*Credential
	RequiredActions []string
}

// Credential represents a user credential (password, OTP, etc.).
type Credential struct {
	Type      string
	Value     string
	Temporary bool
}

// Role represents a role that can be assigned to users.
// Roles can be at the organization level or client level.
type Role struct {
	ID          string
	Name        string
	Description string
	Composite   bool
	ClientRole  bool   // true if client-level, false if organization-level
	ContainerID string // The ID of the organization or client that contains this role
	Attributes  map[string][]string
}
