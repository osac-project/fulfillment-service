/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package keycloak

import (
	"github.com/osac-project/fulfillment-service/internal/idp"
)

// Keycloak-specific API types.
// These map directly to the Keycloak REST API.
// See: https://www.keycloak.org/docs-api/latest/rest-api/index.html

type keycloakUser struct {
	ID              string              `json:"id,omitempty"`
	Username        string              `json:"username,omitempty"`
	Email           string              `json:"email,omitempty"`
	EmailVerified   *bool               `json:"emailVerified,omitempty"`
	Enabled         *bool               `json:"enabled,omitempty"`
	FirstName       string              `json:"firstName,omitempty"`
	LastName        string              `json:"lastName,omitempty"`
	Attributes      map[string][]string `json:"attributes,omitempty"`
	Groups          []string            `json:"groups,omitempty"`
	Credentials     []*keycloakCred     `json:"credentials,omitempty"`
	RequiredActions []string            `json:"requiredActions,omitempty"`
}

type keycloakCred struct {
	Type      string `json:"type,omitempty"`
	Value     string `json:"value,omitempty"`
	Temporary *bool  `json:"temporary,omitempty"`
}

type keycloakClient struct {
	ID       string `json:"id,omitempty"`
	ClientID string `json:"clientId,omitempty"`
}

type keycloakRole struct {
	ID          string              `json:"id,omitempty"`
	Name        string              `json:"name,omitempty"`
	Description string              `json:"description,omitempty"`
	Composite   *bool               `json:"composite,omitempty"`
	ClientRole  *bool               `json:"clientRole,omitempty"`
	ContainerID string              `json:"containerId,omitempty"`
	Attributes  map[string][]string `json:"attributes,omitempty"`
}

type keycloakOrganization struct {
	ID         string                        `json:"id,omitempty"`
	Name       string                        `json:"name,omitempty"`
	Alias      string                        `json:"alias,omitempty"`
	Enabled    *bool                         `json:"enabled,omitempty"`
	Attributes map[string][]string           `json:"attributes,omitempty"`
	Domains    []*keycloakOrganizationDomain `json:"domains,omitempty"`
}

type keycloakOrganizationDomain struct {
	Name     string `json:"name,omitempty"`
	Verified bool   `json:"verified,omitempty"`
}

// Conversion functions from generic types to Keycloak types

func toKeycloakUser(user *idp.User) *keycloakUser {
	emailVerified := user.EmailVerified
	enabled := user.Enabled

	var creds []*keycloakCred
	for _, cred := range user.Credentials {
		temporary := cred.Temporary
		creds = append(creds, &keycloakCred{
			Type:      cred.Type,
			Value:     cred.Value,
			Temporary: &temporary,
		})
	}

	return &keycloakUser{
		ID:              user.ID,
		Username:        user.Username,
		Email:           user.Email,
		EmailVerified:   &emailVerified,
		Enabled:         &enabled,
		FirstName:       user.FirstName,
		LastName:        user.LastName,
		Attributes:      user.Attributes,
		Groups:          user.Groups,
		Credentials:     creds,
		RequiredActions: user.RequiredActions,
	}
}

func fromKeycloakUser(kcUser *keycloakUser) *idp.User {
	emailVerified := false
	if kcUser.EmailVerified != nil {
		emailVerified = *kcUser.EmailVerified
	}
	enabled := false
	if kcUser.Enabled != nil {
		enabled = *kcUser.Enabled
	}

	var creds []*idp.Credential
	for _, kcCred := range kcUser.Credentials {
		temporary := false
		if kcCred.Temporary != nil {
			temporary = *kcCred.Temporary
		}
		creds = append(creds, &idp.Credential{
			Type:      kcCred.Type,
			Value:     kcCred.Value,
			Temporary: temporary,
		})
	}

	return &idp.User{
		ID:              kcUser.ID,
		Username:        kcUser.Username,
		Email:           kcUser.Email,
		EmailVerified:   emailVerified,
		Enabled:         enabled,
		FirstName:       kcUser.FirstName,
		LastName:        kcUser.LastName,
		Attributes:      kcUser.Attributes,
		Groups:          kcUser.Groups,
		Credentials:     creds,
		RequiredActions: kcUser.RequiredActions,
	}
}

func toKeycloakRole(role *idp.Role) *keycloakRole {
	composite := role.Composite
	clientRole := role.ClientRole

	return &keycloakRole{
		ID:          role.ID,
		Name:        role.Name,
		Description: role.Description,
		Composite:   &composite,
		ClientRole:  &clientRole,
		ContainerID: role.ContainerID,
		Attributes:  role.Attributes,
	}
}

func fromKeycloakRole(kcRole *keycloakRole) *idp.Role {
	composite := false
	if kcRole.Composite != nil {
		composite = *kcRole.Composite
	}
	clientRole := false
	if kcRole.ClientRole != nil {
		clientRole = *kcRole.ClientRole
	}

	return &idp.Role{
		ID:          kcRole.ID,
		Name:        kcRole.Name,
		Description: kcRole.Description,
		Composite:   composite,
		ClientRole:  clientRole,
		ContainerID: kcRole.ContainerID,
		Attributes:  kcRole.Attributes,
	}
}

func toKeycloakOrganization(org *idp.Organization) *keycloakOrganization {
	enabled := org.Enabled
	var domains []*keycloakOrganizationDomain
	for _, d := range org.Domains {
		domains = append(domains, &keycloakOrganizationDomain{Name: d})
	}
	return &keycloakOrganization{
		ID:         org.ID,
		Name:       org.Name,
		Enabled:    &enabled,
		Attributes: org.Attributes,
		Domains:    domains,
	}
}

func fromKeycloakOrganization(kcOrg *keycloakOrganization) *idp.Organization {
	enabled := false
	if kcOrg.Enabled != nil {
		enabled = *kcOrg.Enabled
	}
	// Use Alias as DisplayName if Name is not suitable for display
	displayName := kcOrg.Alias
	if displayName == "" {
		displayName = kcOrg.Name
	}
	var domains []string
	for _, d := range kcOrg.Domains {
		if d == nil {
			continue
		}
		domains = append(domains, d.Name)
	}
	return &idp.Organization{
		ID:          kcOrg.ID,
		Name:        kcOrg.Name,
		DisplayName: displayName,
		Enabled:     enabled,
		Domains:     domains,
		Attributes:  kcOrg.Attributes,
	}
}

// Authorization Services types
// These map to Keycloak Authorization Services (UMA 2.0) REST API.
// See: https://www.keycloak.org/docs/latest/authorization_services/

// keycloakAuthorizationResource represents a protected resource in Keycloak Authorization Services.
type keycloakAuthorizationScope struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type keycloakAuthorizationResource struct {
	ID         string                       `json:"_id,omitempty"`
	Name       string                       `json:"name,omitempty"`
	Type       string                       `json:"type,omitempty"`
	Scopes     []keycloakAuthorizationScope `json:"scopes,omitempty"`
	URIs       []string                     `json:"uris,omitempty"`
	Attributes map[string][]string          `json:"attributes,omitempty"`
}

// Conversion functions for authorization resources
func toKeycloakAuthorizationResource(resource *idp.AuthorizationResource) *keycloakAuthorizationResource {
	scopes := make([]keycloakAuthorizationScope, len(resource.Scopes))
	for i, scopeName := range resource.Scopes {
		scopes[i] = keycloakAuthorizationScope{Name: scopeName}
	}
	return &keycloakAuthorizationResource{
		ID:         resource.ID,
		Name:       resource.Name,
		Type:       resource.Type,
		Scopes:     scopes,
		URIs:       resource.URIs,
		Attributes: resource.Attributes,
	}
}

func fromKeycloakAuthorizationResource(kcResource *keycloakAuthorizationResource) *idp.AuthorizationResource {
	scopeNames := make([]string, len(kcResource.Scopes))
	for i, scope := range kcResource.Scopes {
		scopeNames[i] = scope.Name
	}
	return &idp.AuthorizationResource{
		ID:         kcResource.ID,
		Name:       kcResource.Name,
		Type:       kcResource.Type,
		Scopes:     scopeNames,
		URIs:       kcResource.URIs,
		Attributes: kcResource.Attributes,
	}
}

// Identity Provider types
// These map to Keycloak Identity Provider REST API.
// See: https://www.keycloak.org/docs-api/latest/rest-api/index.html#_identity_providers_resource

// keycloakIdentityProvider represents an external identity provider configuration in Keycloak.
// Identity providers are configured at the realm level and can be linked to specific organizations.
type keycloakIdentityProvider struct {
	Alias       string            `json:"alias,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
	InternalID  string            `json:"internalId,omitempty"`
	ProviderID  string            `json:"providerId,omitempty"` // "ldap", "oidc", "saml", etc.
	Enabled     bool              `json:"enabled,omitempty"`
	Config      map[string]string `json:"config,omitempty"` // Provider-specific configuration
}

// Conversion functions for identity providers
func toKeycloakIdentityProvider(idpProvider *idp.IdentityProvider) *keycloakIdentityProvider {
	if idpProvider == nil {
		return nil
	}

	return &keycloakIdentityProvider{
		Alias:       idpProvider.Alias,
		DisplayName: idpProvider.DisplayName,
		ProviderID:  idpProvider.Type,
		Enabled:     idpProvider.Enabled,
		Config:      idpProvider.Config,
	}
}

func fromKeycloakIdentityProvider(kcIdp *keycloakIdentityProvider) *idp.IdentityProvider {
	if kcIdp == nil {
		return nil
	}

	return &idp.IdentityProvider{
		Alias:       kcIdp.Alias,
		DisplayName: kcIdp.DisplayName,
		Type:        kcIdp.ProviderID,
		Enabled:     kcIdp.Enabled,
		Config:      kcIdp.Config,
	}
}
