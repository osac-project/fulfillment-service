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
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/osac-project/fulfillment-service/internal/apiclient"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

// Client is a Keycloak-specific implementation of the idp.Client interface.
type Client struct {
	logger     *slog.Logger
	httpClient *apiclient.Client

	// realm-management client UUIDs by organization name
	realmManagementUUIDs map[string]string
	mu                   sync.RWMutex
}

// ClientBuilder builds a Keycloak client.
type ClientBuilder struct {
	logger      *slog.Logger
	baseURL     string
	tokenSource auth.TokenSource
	caPool      *x509.CertPool
	httpClient  *http.Client
}

// Ensure Client implements idp.Client at compile time
var _ idp.Client = (*Client)(nil)

// NewClient creates a builder for a Keycloak admin client.
func NewClient() *ClientBuilder {
	return &ClientBuilder{}
}

// SetLogger sets the logger.
func (b *ClientBuilder) SetLogger(value *slog.Logger) *ClientBuilder {
	b.logger = value
	return b
}

// SetBaseURL sets the base URL of the Keycloak server.
func (b *ClientBuilder) SetBaseURL(value string) *ClientBuilder {
	b.baseURL = value
	return b
}

// SetTokenSource sets the token source for authentication.
func (b *ClientBuilder) SetTokenSource(value auth.TokenSource) *ClientBuilder {
	b.tokenSource = value
	return b
}

// SetCaPool sets the CA certificate pool.
func (b *ClientBuilder) SetCaPool(value *x509.CertPool) *ClientBuilder {
	b.caPool = value
	return b
}

// SetHTTPClient sets a custom HTTP client.
func (b *ClientBuilder) SetHTTPClient(value *http.Client) *ClientBuilder {
	b.httpClient = value
	return b
}

// Build creates the Keycloak client.
func (b *ClientBuilder) Build() (result *Client, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.baseURL == "" {
		err = errors.New("base URL is mandatory")
		return
	}
	if b.tokenSource == nil {
		err = errors.New("token source is mandatory")
		return
	}

	// Build the underlying HTTP client
	httpClientBuilder := apiclient.NewClient().
		SetLogger(b.logger).
		SetBaseURL(strings.TrimSuffix(b.baseURL, "/")).
		SetTokenSource(b.tokenSource)

	if b.caPool != nil {
		httpClientBuilder = httpClientBuilder.SetCaPool(b.caPool)
	}
	if b.httpClient != nil {
		httpClientBuilder = httpClientBuilder.SetHTTPClient(b.httpClient)
	}

	httpClient, err := httpClientBuilder.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP client: %w", err)
	}

	result = &Client{
		logger:               b.logger,
		httpClient:           httpClient,
		realmManagementUUIDs: make(map[string]string),
	}
	return
}

// CreateOrganization creates a new organization (Keycloak realm).
// Returns the created organization with server-assigned ID and any server defaults.
func (c *Client) CreateOrganization(ctx context.Context, org *idp.Organization) (*idp.Organization, error) {
	kcRealm := toKeycloakRealm(org)
	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, "/admin/realms", kcRealm)
	if err != nil {
		var apiErr *apiclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
			return nil, &idp.ConflictError{
				ResourceType: idp.ResourceTypeOrganization,
				ResourceName: org.Name,
				Err:          err,
			}
		}
		return nil, fmt.Errorf("failed to create organization: %w", err)
	}
	response.Body.Close()

	// Keycloak's POST /admin/realms returns 201 with no body, so we fetch the created realm
	// to get the server-assigned ID and verify the realm was actually created
	return c.GetOrganization(ctx, org.Name)
}

// GetOrganization retrieves an organization (Keycloak realm) by name.
func (c *Client) GetOrganization(ctx context.Context, name string) (*idp.Organization, error) {
	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s", url.PathEscape(name)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization: %w", err)
	}
	defer response.Body.Close()

	var kcRealm keycloakRealm
	if err = json.NewDecoder(response.Body).Decode(&kcRealm); err != nil {
		return nil, fmt.Errorf("failed to decode organization response: %w", err)
	}
	return fromKeycloakRealm(&kcRealm), nil
}

// DeleteOrganization deletes an organization (Keycloak realm) by name.
func (c *Client) DeleteOrganization(ctx context.Context, organizationName string) error {
	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s", url.PathEscape(organizationName)), nil)
	if err != nil {
		return fmt.Errorf("failed to delete organization: %w", err)
	}
	defer response.Body.Close()

	// Clear cached realm-management UUID for this organization to prevent stale data
	// if the organization is recreated later
	c.mu.Lock()
	delete(c.realmManagementUUIDs, organizationName)
	c.mu.Unlock()

	return nil
}

// CreateUser creates a new user in an organization.
// Returns the created user with ID populated.
func (c *Client) CreateUser(ctx context.Context, organizationName string, user *idp.User) (*idp.User, error) {
	kcUser := toKeycloakUser(user)
	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users", url.PathEscape(organizationName)), kcUser)
	if err != nil {
		var apiErr *apiclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict {
			return nil, &idp.ConflictError{
				ResourceType: idp.ResourceTypeUser,
				ResourceName: user.Username,
				Err:          err,
			}
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	defer response.Body.Close()

	// Extract user ID from Location header
	// Location format: https://keycloak.example.com/admin/realms/{realm}/users/{user-id}
	location := response.Header.Get("Location")
	if location == "" {
		return nil, fmt.Errorf("Location header not present in create user response")
	}

	// Extract the last segment of the URL path as the user ID
	parts := strings.Split(strings.TrimSuffix(location, "/"), "/")
	if len(parts) == 0 {
		return nil, fmt.Errorf("failed to extract user ID from Location header: %s", location)
	}

	userID := parts[len(parts)-1]
	if userID == "" {
		return nil, fmt.Errorf("extracted user ID is empty from Location header: %s", location)
	}

	// Return a copy of the user with ID populated
	createdUser := &idp.User{
		ID:              userID,
		Username:        user.Username,
		Email:           user.Email,
		EmailVerified:   user.EmailVerified,
		Enabled:         user.Enabled,
		FirstName:       user.FirstName,
		LastName:        user.LastName,
		Attributes:      user.Attributes,
		Groups:          user.Groups,
		Credentials:     user.Credentials,
		RequiredActions: user.RequiredActions,
	}

	return createdUser, nil
}

// GetUser retrieves a user by ID.
func (c *Client) GetUser(ctx context.Context, organizationName, userID string) (*idp.User, error) {
	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/users/%s", url.PathEscape(organizationName), url.PathEscape(userID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	defer response.Body.Close()

	var kcUser keycloakUser
	if err = json.NewDecoder(response.Body).Decode(&kcUser); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}
	return fromKeycloakUser(&kcUser), nil
}

// ListUsers lists all users in an organization.
func (c *Client) ListUsers(ctx context.Context, organizationName string) ([]*idp.User, error) {
	var allUsers []*idp.User
	const maxPerPage = 100
	first := 0

	// Fetches all pages to ensure no users are missed due to Keycloak's pagination.
	for {
		// Check if context is cancelled before making the next API call
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Fetch one page of users
		path := fmt.Sprintf("/admin/realms/%s/users?first=%d&max=%d",
			url.PathEscape(organizationName), first, maxPerPage)

		response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list users: %w", err)
		}

		var kcUsers []keycloakUser
		err = json.NewDecoder(response.Body).Decode(&kcUsers)
		response.Body.Close()

		if err != nil {
			return nil, fmt.Errorf("failed to decode users response: %w", err)
		}

		// Convert and append this page
		for _, kcUser := range kcUsers {
			allUsers = append(allUsers, fromKeycloakUser(&kcUser))
		}

		// If we got fewer than max, we've reached the last page
		if len(kcUsers) < maxPerPage {
			break
		}

		// Move to next page
		first += maxPerPage
	}

	return allUsers, nil
}

// DeleteUser deletes a user by ID.
func (c *Client) DeleteUser(ctx context.Context, organizationName, userID string) error {
	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/users/%s", url.PathEscape(organizationName), url.PathEscape(userID)), nil)
	if err != nil {
		var apiErr *apiclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("user %q not found in organization %q: %w", userID, organizationName, err)
		}
		return fmt.Errorf("failed to delete user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// ListOrganizationRoles lists all organization-level (realm) roles.
func (c *Client) ListOrganizationRoles(ctx context.Context, organizationName string) ([]*idp.Role, error) {
	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/roles", url.PathEscape(organizationName)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list realm roles: %w", err)
	}
	defer response.Body.Close()

	var kcRoles []keycloakRole
	if err = json.NewDecoder(response.Body).Decode(&kcRoles); err != nil {
		return nil, fmt.Errorf("failed to decode realm roles response: %w", err)
	}

	roles := make([]*idp.Role, len(kcRoles))
	for i, kcRole := range kcRoles {
		roles[i] = fromKeycloakRole(&kcRole)
	}
	return roles, nil
}

// ListClientRoles lists all roles for a specific client.
//
// The clientID parameter accepts either format for convenience:
//   - Human-readable clientId: "realm-management", "account", "my-app"
//   - Internal UUID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
func (c *Client) ListClientRoles(ctx context.Context, organizationName, clientID string) ([]*idp.Role, error) {
	// Resolve to internal UUID
	internalID, err := c.GetClientByClientID(ctx, organizationName, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve client ID: %w", err)
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/clients/%s/roles", url.PathEscape(organizationName), url.PathEscape(internalID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list client roles: %w", err)
	}
	defer response.Body.Close()

	var kcRoles []keycloakRole
	if err = json.NewDecoder(response.Body).Decode(&kcRoles); err != nil {
		return nil, fmt.Errorf("failed to decode client roles response: %w", err)
	}

	roles := make([]*idp.Role, len(kcRoles))
	for i, kcRole := range kcRoles {
		roles[i] = fromKeycloakRole(&kcRole)
	}
	return roles, nil
}

// AssignOrganizationRolesToUser adds organization-level (realm) roles to a user.
func (c *Client) AssignOrganizationRolesToUser(ctx context.Context, organizationName, userID string, roles []*idp.Role) error {
	kcRoles := make([]keycloakRole, len(roles))
	for i, role := range roles {
		kcRoles[i] = *toKeycloakRole(role)
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm", url.PathEscape(organizationName), url.PathEscape(userID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to assign realm roles to user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// AssignClientRolesToUser adds client-level roles to a user.
//
// The clientID parameter accepts either format:
//   - Human-readable clientId: "realm-management", "account", "my-app"
//   - Internal UUID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
func (c *Client) AssignClientRolesToUser(ctx context.Context, organizationName, userID, clientID string, roles []*idp.Role) error {
	// Resolve to internal UUID
	internalID, err := c.GetClientByClientID(ctx, organizationName, clientID)
	if err != nil {
		return fmt.Errorf("failed to resolve client ID: %w", err)
	}

	kcRoles := make([]keycloakRole, len(roles))
	for i, role := range roles {
		kcRoles[i] = *toKeycloakRole(role)
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/clients/%s", url.PathEscape(organizationName), url.PathEscape(userID), url.PathEscape(internalID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to assign client roles to user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// RemoveOrganizationRolesFromUser removes organization-level (realm) roles from a user.
func (c *Client) RemoveOrganizationRolesFromUser(ctx context.Context, organizationName, userID string, roles []*idp.Role) error {
	kcRoles := make([]keycloakRole, len(roles))
	for i, role := range roles {
		kcRoles[i] = *toKeycloakRole(role)
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm", url.PathEscape(organizationName), url.PathEscape(userID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to remove realm roles from user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// RemoveClientRolesFromUser removes client-level roles from a user.
//
// The clientID parameter accepts either format:
//   - Human-readable clientId: "realm-management", "account", "my-app"
//   - Internal UUID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
func (c *Client) RemoveClientRolesFromUser(ctx context.Context, organizationName, userID, clientID string, roles []*idp.Role) error {
	// Resolve to internal UUID
	internalID, err := c.GetClientByClientID(ctx, organizationName, clientID)
	if err != nil {
		return fmt.Errorf("failed to resolve client ID: %w", err)
	}

	kcRoles := make([]keycloakRole, len(roles))
	for i, role := range roles {
		kcRoles[i] = *toKeycloakRole(role)
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/clients/%s", url.PathEscape(organizationName), url.PathEscape(userID), url.PathEscape(internalID)), kcRoles)
	if err != nil {
		return fmt.Errorf("failed to remove client roles from user: %w", err)
	}
	defer response.Body.Close()
	return nil
}

// GetUserOrganizationRoles gets the organization-level (realm) roles assigned to a user.
func (c *Client) GetUserOrganizationRoles(ctx context.Context, organizationName, userID string) ([]*idp.Role, error) {
	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/realm", url.PathEscape(organizationName), url.PathEscape(userID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get user realm roles: %w", err)
	}
	defer response.Body.Close()

	var kcRoles []keycloakRole
	if err = json.NewDecoder(response.Body).Decode(&kcRoles); err != nil {
		return nil, fmt.Errorf("failed to decode user realm roles response: %w", err)
	}

	roles := make([]*idp.Role, len(kcRoles))
	for i, kcRole := range kcRoles {
		roles[i] = fromKeycloakRole(&kcRole)
	}
	return roles, nil
}

// GetUserClientRoles gets the client-level roles assigned to a user.
//
// The clientID parameter accepts either format:
//   - Human-readable clientId: "realm-management", "account", "my-app"
//   - Internal UUID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
func (c *Client) GetUserClientRoles(ctx context.Context, organizationName, userID, clientID string) ([]*idp.Role, error) {
	// Resolve to internal UUID
	internalID, err := c.GetClientByClientID(ctx, organizationName, clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve client ID: %w", err)
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/users/%s/role-mappings/clients/%s", url.PathEscape(organizationName), url.PathEscape(userID), url.PathEscape(internalID)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get user client roles: %w", err)
	}
	defer response.Body.Close()

	var kcRoles []keycloakRole
	if err = json.NewDecoder(response.Body).Decode(&kcRoles); err != nil {
		return nil, fmt.Errorf("failed to decode user client roles response: %w", err)
	}

	roles := make([]*idp.Role, len(kcRoles))
	for i, kcRole := range kcRoles {
		roles[i] = fromKeycloakRole(&kcRole)
	}
	return roles, nil
}

// GetClientByClientID resolves a client identifier to its internal UUID.
//
// The clientID parameter accepts either format:
//   - Human-readable clientId: "realm-management", "account", "my-app"
//   - Internal UUID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
//
// The method first checks if clientID is a valid UUID. If so, it returns it immediately
// (no API call needed). For the "realm-management" client (the only client we currently use),
// the internal UUID is looked up once per organization and stored.
//
// This is needed because Keycloak's role-mapping API endpoints require the internal UUID,
// but we use the human-readable clientId "realm-management".
//
// Example:
//
//	uuid, err := client.GetClientByClientID(ctx, "my-org", "realm-management")
//	// Returns: "a1b2c3d4-e5f6-7890-..." (internal UUID)
func (c *Client) GetClientByClientID(ctx context.Context, organizationName, clientID string) (string, error) {
	// Check if clientID is already a valid UUID (internal ID)
	// If so, return it immediately without making an API call
	if _, err := uuid.Parse(clientID); err == nil {
		return clientID, nil
	}

	// For realm-management, check if we already have the UUID for this organization
	if clientID == realmManagementClientID {
		c.mu.RLock()
		if storedUUID, found := c.realmManagementUUIDs[organizationName]; found {
			c.mu.RUnlock()
			return storedUUID, nil
		}
		c.mu.RUnlock()
	}

	// Look up the client UUID via API
	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/admin/realms/%s/clients?clientId=%s", url.PathEscape(organizationName), url.QueryEscape(clientID)), nil)
	if err != nil {
		return "", fmt.Errorf("failed to get client by clientId: %w", err)
	}
	defer response.Body.Close()

	var kcClients []keycloakClient
	if err = json.NewDecoder(response.Body).Decode(&kcClients); err != nil {
		return "", fmt.Errorf("failed to decode clients response: %w", err)
	}

	if len(kcClients) == 0 {
		return "", fmt.Errorf("client %q not found", clientID)
	}

	internalUUID := kcClients[0].ID

	// Save realm-management UUID for this organization
	if clientID == realmManagementClientID {
		c.mu.Lock()
		c.realmManagementUUIDs[organizationName] = internalUUID
		c.mu.Unlock()
	}

	return internalUUID, nil
}

// AssignOrganizationAdminPermissions grants full administrative access to an organization for the specified user.
//
// For Keycloak, this assigns client roles from the built-in "realm-management" client.
// NOTE: Keycloak's administrative permissions are NOT organization roles - they are client-level roles
// on the "realm-management" client that exists by default in every realm. This is why we use
// AssignClientRolesToUser instead of AssignOrganizationRolesToUser.
func (c *Client) AssignOrganizationAdminPermissions(ctx context.Context, organizationName, userID string) error {
	c.logger.DebugContext(ctx, "Assigning organization admin permissions",
		slog.String("organization", organizationName),
		slog.String("user_id", userID),
	)

	// Get all roles from the built-in realm-management client.
	// This is a special Keycloak client (not one we create) that exists by default
	// and contains all administrative roles for the realm.
	roles, err := c.ListClientRoles(ctx, organizationName, realmManagementClientID)
	if err != nil {
		return fmt.Errorf("failed to list realm-management roles: %w", err)
	}

	// Filter for the key management roles
	var rolesToAssign []*idp.Role
	for _, role := range roles {
		if slices.Contains(keycloakRealmManagementRoles, role.Name) {
			rolesToAssign = append(rolesToAssign, role)
		}
	}

	if len(rolesToAssign) == 0 {
		return fmt.Errorf("no realm management roles found to assign for organization %s", organizationName)
	}

	// Assign the client roles to the user.
	// These are client-level roles from the "realm-management" client, not organization-level roles.
	err = c.AssignClientRolesToUser(ctx, organizationName, userID, realmManagementClientID, rolesToAssign)
	if err != nil {
		return fmt.Errorf("failed to assign realm management roles: %w", err)
	}

	c.logger.InfoContext(ctx, "Organization admin permissions assigned",
		slog.String("organization", organizationName),
		slog.String("user_id", userID),
		slog.Int("role_count", len(rolesToAssign)),
	)
	return nil
}

// AssignIdpManagerPermissions grants limited IdP management permissions to the specified user.
//
// For Keycloak, this assigns a limited set of client roles from the built-in "realm-management" client.
// The break-glass account can manage users and identity providers but cannot modify critical
// realm settings, clients, or authorization policies.
func (c *Client) AssignIdpManagerPermissions(ctx context.Context, organizationName, userID string) error {
	c.logger.DebugContext(ctx, "Assigning IdP manager permissions",
		slog.String("organization", organizationName),
		slog.String("user_id", userID),
	)

	// Get all roles from the built-in realm-management client
	roles, err := c.ListClientRoles(ctx, organizationName, realmManagementClientID)
	if err != nil {
		return fmt.Errorf("failed to list realm-management roles: %w", err)
	}

	// Filter for the limited IdP manager roles
	var rolesToAssign []*idp.Role
	for _, role := range roles {
		if slices.Contains(keycloakIdpManagerRoles, role.Name) {
			rolesToAssign = append(rolesToAssign, role)
		}
	}

	if len(rolesToAssign) == 0 {
		return fmt.Errorf("no IdP manager roles found to assign for organization %s", organizationName)
	}

	// Assign the client roles to the user
	err = c.AssignClientRolesToUser(ctx, organizationName, userID, realmManagementClientID, rolesToAssign)
	if err != nil {
		return fmt.Errorf("failed to assign IdP manager roles: %w", err)
	}

	c.logger.InfoContext(ctx, "IdP manager permissions assigned",
		slog.String("organization", organizationName),
		slog.String("user_id", userID),
		slog.Int("role_count", len(rolesToAssign)),
	)
	return nil
}
