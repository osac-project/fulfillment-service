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

// AuthorizationResource represents a protected resource in an authorization system.
type AuthorizationResource struct {
	// ID is the unique identifier assigned by the authorization system
	ID string

	// Name is the resource name (e.g., "PROJECT-acme-web-app")
	Name string

	// Type is the resource type (e.g., "urn:osac:resources:project")
	Type string

	// Scopes are the actions that can be performed on this resource
	Scopes []string

	// URIs are optional resource URIs
	URIs []string

	// Attributes for additional metadata
	Attributes map[string][]string
}

// AuthorizationPolicy represents a policy in an authorization system.
// Policies define the conditions under which access is granted.
type AuthorizationPolicy struct {
	// ID is the unique identifier assigned by the authorization system
	ID string

	// Name is the policy name (e.g., "my-project-viewers-policy")
	Name string

	// Type is the policy type (e.g., "group", "role", "user", "time", "js")
	Type string

	// Logic is the policy decision strategy ("POSITIVE" or "NEGATIVE")
	// POSITIVE: policy grants access when conditions are met
	// NEGATIVE: policy denies access when conditions are met
	Logic string

	// DecisionStrategy is how multiple policies are evaluated ("UNANIMOUS", "AFFIRMATIVE", "CONSENSUS")
	DecisionStrategy string

	// GroupsClaim is the claim in the token that contains group membership (for group policies)
	GroupsClaim string

	// Groups are the group paths that this policy applies to (for group policies)
	Groups []string
}

// AuthorizationPermission represents a permission that connects policies to resources/scopes.
type AuthorizationPermission struct {
	// ID is the unique identifier assigned by the authorization system
	ID string

	// Name is the permission name (e.g., "my-project-view-permission")
	Name string

	// Type is the permission type (e.g., "scope", "resource")
	Type string

	// Logic is the permission decision strategy ("POSITIVE" or "NEGATIVE")
	Logic string

	// DecisionStrategy is how multiple policies are evaluated ("UNANIMOUS", "AFFIRMATIVE", "CONSENSUS")
	DecisionStrategy string

	// ResourceID is the ID of the resource this permission applies to
	ResourceID string

	// Scopes are the scope names this permission grants
	Scopes []string

	// Policies are the policy IDs that must evaluate to true for this permission
	Policies []string
}

// IdentityProvider represents an external identity provider configuration.
// This represents the connection to an upstream IdP (LDAP/AD/OIDC/SAML/etc) that
// users authenticate against.
type IdentityProvider struct {
	// Alias is the unique name for this IdP within the realm
	Alias string

	// DisplayName is the human-readable name for this IdP
	DisplayName string

	// Type is the IdP provider type as a free-form string.
	// Common values: "ldap", "kerberos", "oidc", "saml", "google", "github", "facebook", "microsoft"
	// The exact set of supported values depends on the underlying IdP system (e.g., Keycloak).
	Type string

	// Enabled indicates whether this IdP is active
	Enabled bool

	// Config contains provider-specific configuration settings.
	// Secrets are automatically filtered by the IdP system and never returned in GET responses.
	// Example keys:
	// - LDAP: connectionUrl, bindDn, usersDn, authType, vendor
	// - OIDC: authorizationUrl, tokenUrl, clientId, issuer, defaultScope
	// - SAML: singleSignOnServiceUrl, singleLogoutServiceUrl, signingCertificate
	Config map[string]string
}
