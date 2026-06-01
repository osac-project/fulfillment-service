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
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/osac-project/fulfillment-service/internal/idp"
)

// CreateGroupPolicy creates a group-based authorization policy in Keycloak.
// The policy evaluates to PERMIT if the user is a member of any of the specified groups.
func (c *Client) CreateGroupPolicy(ctx context.Context, policy *idp.AuthorizationPolicy) (*idp.AuthorizationPolicy, error) {
	if policy == nil {
		return nil, fmt.Errorf("policy is nil")
	}

	c.logger.InfoContext(ctx, "Creating group authorization policy",
		slog.String("policy_name", policy.Name),
		slog.Int("group_count", len(policy.Groups)),
	)

	clientInternalID, err := c.getAuthorizationClientUUID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get authorization client ID: %w", err)
	}

	// Set defaults if not provided
	if policy.Logic == "" {
		policy.Logic = "POSITIVE"
	}
	if policy.DecisionStrategy == "" {
		policy.DecisionStrategy = "UNANIMOUS"
	}
	if policy.GroupsClaim == "" {
		policy.GroupsClaim = ""
	}

	kcPolicy := toKeycloakGroupPolicy(policy)

	path := fmt.Sprintf("/admin/realms/%s/clients/%s/authz/resource-server/policy/group",
		url.PathEscape(c.realmName),
		url.PathEscape(clientInternalID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, path, kcPolicy)
	if err != nil {
		return nil, fmt.Errorf("failed to create group policy: %w", err)
	}
	defer response.Body.Close()

	var createdPolicy keycloakGroupPolicy
	if err := json.NewDecoder(response.Body).Decode(&createdPolicy); err != nil {
		return nil, fmt.Errorf("failed to decode group policy response: %w", err)
	}

	c.logger.InfoContext(ctx, "Created group authorization policy",
		slog.String("policy_id", createdPolicy.ID),
		slog.String("policy_name", createdPolicy.Name),
	)

	return fromKeycloakGroupPolicy(&createdPolicy), nil
}

// DeletePolicy deletes an authorization policy by ID.
func (c *Client) DeletePolicy(ctx context.Context, policyID string) error {
	c.logger.InfoContext(ctx, "Deleting authorization policy",
		slog.String("policy_id", policyID),
	)

	clientInternalID, err := c.getAuthorizationClientUUID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get authorization client ID: %w", err)
	}

	path := fmt.Sprintf("/admin/realms/%s/clients/%s/authz/resource-server/policy/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(clientInternalID),
		url.PathEscape(policyID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete policy: %w", err)
	}
	defer response.Body.Close()

	c.logger.InfoContext(ctx, "Deleted authorization policy",
		slog.String("policy_id", policyID),
	)

	return nil
}

// CreateScopePermission creates a scope-based permission that connects policies to resource scopes.
func (c *Client) CreateScopePermission(ctx context.Context, permission *idp.AuthorizationPermission) (*idp.AuthorizationPermission, error) {
	if permission == nil {
		return nil, fmt.Errorf("permission is nil")
	}

	c.logger.InfoContext(ctx, "Creating scope permission",
		slog.String("permission_name", permission.Name),
		slog.Int("scope_count", len(permission.Scopes)),
		slog.Int("policy_count", len(permission.Policies)),
	)

	clientInternalID, err := c.getAuthorizationClientUUID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get authorization client ID: %w", err)
	}

	// Set defaults if not provided
	if permission.Logic == "" {
		permission.Logic = "POSITIVE"
	}
	if permission.DecisionStrategy == "" {
		permission.DecisionStrategy = "UNANIMOUS"
	}

	// Convert scope names to scope IDs
	scopeIDs := make([]string, 0, len(permission.Scopes))
	for _, scopeName := range permission.Scopes {
		scopeID, err := c.getScopeID(ctx, clientInternalID, scopeName)
		if err != nil {
			return nil, fmt.Errorf("failed to get scope ID for %s: %w", scopeName, err)
		}
		scopeIDs = append(scopeIDs, scopeID)
	}

	kcPermission := toKeycloakScopePermission(permission)
	// Replace scope names with scope IDs for Keycloak API
	kcPermission.Scopes = scopeIDs
	// For creating permissions, Keycloak expects resources as an array
	if kcPermission.ResourceID != "" {
		kcPermission.Resources = []string{kcPermission.ResourceID}
		kcPermission.ResourceID = "" // Don't send the single resource field
	}

	path := fmt.Sprintf("/admin/realms/%s/clients/%s/authz/resource-server/permission/scope",
		url.PathEscape(c.realmName),
		url.PathEscape(clientInternalID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, path, kcPermission)
	if err != nil {
		return nil, fmt.Errorf("failed to create scope permission: %w", err)
	}
	defer response.Body.Close()

	var createdPermission keycloakScopePermission
	if err := json.NewDecoder(response.Body).Decode(&createdPermission); err != nil {
		return nil, fmt.Errorf("failed to decode scope permission response: %w", err)
	}

	c.logger.InfoContext(ctx, "Created scope permission",
		slog.String("permission_id", createdPermission.ID),
		slog.String("permission_name", createdPermission.Name),
	)

	result := fromKeycloakScopePermission(&createdPermission)
	// Restore the original scope names (not IDs) in the result
	result.Scopes = permission.Scopes

	return result, nil
}

// DeletePermission deletes an authorization permission by ID.
func (c *Client) DeletePermission(ctx context.Context, permissionID string) error {
	c.logger.InfoContext(ctx, "Deleting authorization permission",
		slog.String("permission_id", permissionID),
	)

	clientInternalID, err := c.getAuthorizationClientUUID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get authorization client ID: %w", err)
	}

	path := fmt.Sprintf("/admin/realms/%s/clients/%s/authz/resource-server/permission/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(clientInternalID),
		url.PathEscape(permissionID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete permission: %w", err)
	}
	defer response.Body.Close()

	c.logger.InfoContext(ctx, "Deleted authorization permission",
		slog.String("permission_id", permissionID),
	)

	return nil
}

// GetPolicyIDByName gets an authorization policy ID by its name.
// This is a helper method used by ResourceManager for cleanup operations.
func (c *Client) GetPolicyIDByName(ctx context.Context, policyName string) (string, error) {
	clientInternalID, err := c.getAuthorizationClientUUID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get authorization client ID: %w", err)
	}

	path := fmt.Sprintf("/admin/realms/%s/clients/%s/authz/resource-server/policy?name=%s",
		url.PathEscape(c.realmName),
		url.PathEscape(clientInternalID),
		url.QueryEscape(policyName),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get policy: %w", err)
	}
	defer response.Body.Close()

	var policies []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&policies); err != nil {
		return "", fmt.Errorf("failed to decode policies: %w", err)
	}

	if len(policies) == 0 {
		return "", fmt.Errorf("policy %s not found", policyName)
	}

	return policies[0].ID, nil
}

// GetPermissionIDByName gets an authorization permission ID by its name.
// This is a helper method used by ResourceManager for cleanup operations.
func (c *Client) GetPermissionIDByName(ctx context.Context, permissionName string) (string, error) {
	clientInternalID, err := c.getAuthorizationClientUUID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get authorization client ID: %w", err)
	}

	path := fmt.Sprintf("/admin/realms/%s/clients/%s/authz/resource-server/permission?name=%s",
		url.PathEscape(c.realmName),
		url.PathEscape(clientInternalID),
		url.QueryEscape(permissionName),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get permission: %w", err)
	}
	defer response.Body.Close()

	var permissions []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&permissions); err != nil {
		return "", fmt.Errorf("failed to decode permissions: %w", err)
	}

	if len(permissions) == 0 {
		return "", fmt.Errorf("permission %s not found", permissionName)
	}

	return permissions[0].ID, nil
}

func (c *Client) getScopeID(ctx context.Context, clientInternalID, scopeName string) (string, error) {
	path := fmt.Sprintf("/admin/realms/%s/clients/%s/authz/resource-server/scope/search?name=%s",
		url.PathEscape(c.realmName),
		url.PathEscape(clientInternalID),
		url.QueryEscape(scopeName),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get scope: %w", err)
	}
	defer response.Body.Close()

	var scope struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&scope); err != nil {
		return "", fmt.Errorf("failed to decode scope: %w", err)
	}

	if scope.ID == "" {
		return "", fmt.Errorf("scope %s not found", scopeName)
	}

	return scope.ID, nil
}
