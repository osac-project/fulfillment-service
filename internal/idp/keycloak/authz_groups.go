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
)

// CreateAuthorizationGroup creates a Keycloak group for authorization purposes.
// Group path should be in format "/project-{tenant}-{name}-{viewers|managers}"
func (c *Client) CreateAuthorizationGroup(ctx context.Context, groupName, groupPath string) error {
	c.logger.InfoContext(ctx, "Creating authorization group",
		slog.String("groupName", groupName),
		slog.String("groupPath", groupPath),
	)

	path := fmt.Sprintf("/admin/realms/%s/groups", url.PathEscape(c.realmName))

	groupPayload := map[string]interface{}{
		"name": groupName,
		"path": groupPath,
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, path, groupPayload)
	if err != nil {
		return fmt.Errorf("failed to create group: %w", err)
	}
	defer response.Body.Close()

	c.logger.InfoContext(ctx, "Created authorization group",
		slog.String("groupName", groupName),
	)

	return nil
}

// DeleteAuthorizationGroup deletes a Keycloak group by ID.
func (c *Client) DeleteAuthorizationGroup(ctx context.Context, groupID string) error {
	c.logger.InfoContext(ctx, "Deleting authorization group",
		slog.String("groupID", groupID),
	)

	path := fmt.Sprintf("/admin/realms/%s/groups/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(groupID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete group: %w", err)
	}
	defer response.Body.Close()

	c.logger.InfoContext(ctx, "Deleted authorization group",
		slog.String("groupID", groupID),
	)

	return nil
}

// AddUserToAuthorizationGroup adds a user to a Keycloak group.
func (c *Client) AddUserToAuthorizationGroup(ctx context.Context, userID, groupPath string) error {
	c.logger.InfoContext(ctx, "Adding user to authorization group",
		slog.String("userID", userID),
		slog.String("groupPath", groupPath),
	)

	// Get group ID from path
	groupID, err := c.getGroupIDByPath(ctx, groupPath)
	if err != nil {
		return fmt.Errorf("failed to get group ID: %w", err)
	}

	path := fmt.Sprintf("/admin/realms/%s/users/%s/groups/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(userID),
		url.PathEscape(groupID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodPut, path, nil)
	if err != nil {
		return fmt.Errorf("failed to add user to group: %w", err)
	}
	defer response.Body.Close()

	c.logger.InfoContext(ctx, "Added user to authorization group",
		slog.String("userID", userID),
		slog.String("groupPath", groupPath),
	)

	return nil
}

// RemoveUserFromAuthorizationGroup removes a user from a Keycloak group.
func (c *Client) RemoveUserFromAuthorizationGroup(ctx context.Context, userID, groupPath string) error {
	c.logger.InfoContext(ctx, "Removing user from authorization group",
		slog.String("userID", userID),
		slog.String("groupPath", groupPath),
	)

	// Get group ID from path
	groupID, err := c.getGroupIDByPath(ctx, groupPath)
	if err != nil {
		return fmt.Errorf("failed to get group ID: %w", err)
	}

	path := fmt.Sprintf("/admin/realms/%s/users/%s/groups/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(userID),
		url.PathEscape(groupID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to remove user from group: %w", err)
	}
	defer response.Body.Close()

	c.logger.InfoContext(ctx, "Removed user from authorization group",
		slog.String("userID", userID),
		slog.String("groupPath", groupPath),
	)

	return nil
}

// CreateAuthorizationGroupPolicy creates a group-based policy and scope permission for a resource.
// This sets up the policy and permission so that members of the specified group can access the resource.
func (c *Client) CreateAuthorizationGroupPolicy(ctx context.Context, resourceID, resourceName, groupPath, scopeName string) error {
	c.logger.InfoContext(ctx, "Creating authorization group policy",
		slog.String("resourceID", resourceID),
		slog.String("resourceName", resourceName),
		slog.String("groupPath", groupPath),
		slog.String("scope", scopeName),
	)

	// Get the authorization client UUID
	clientUUID, err := c.getAuthorizationClientUUID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get authorization client UUID: %w", err)
	}

	// Get the scope ID
	scopeID, err := c.getScopeID(ctx, clientUUID, scopeName)
	if err != nil {
		return fmt.Errorf("failed to get scope ID: %w", err)
	}

	// Create group-based policy
	policyName := fmt.Sprintf("Group policy for %s - %s", resourceName, scopeName)
	policyID, err := c.createGroupPolicy(ctx, clientUUID, groupPath, policyName)
	if err != nil {
		return fmt.Errorf("failed to create group policy: %w", err)
	}

	// Create scope-based permission
	permissionName := fmt.Sprintf("Permission for %s - %s", resourceName, scopeName)
	err = c.createScopePermission(ctx, clientUUID, permissionName, resourceID, scopeID, policyID)
	if err != nil {
		return fmt.Errorf("failed to create permission: %w", err)
	}

	c.logger.InfoContext(ctx, "Created authorization group policy",
		slog.String("resourceID", resourceID),
		slog.String("scope", scopeName),
	)

	return nil
}

// DeleteAuthorizationGroupPolicy deletes a group-based policy and its permission.
func (c *Client) DeleteAuthorizationGroupPolicy(ctx context.Context, resourceName, scopeName string) error {
	c.logger.InfoContext(ctx, "Deleting authorization group policy",
		slog.String("resourceName", resourceName),
		slog.String("scope", scopeName),
	)

	// Get the authorization client UUID
	clientUUID, err := c.getAuthorizationClientUUID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get authorization client UUID: %w", err)
	}

	// Delete the permission
	permissionName := fmt.Sprintf("Permission for %s - %s", resourceName, scopeName)
	err = c.deletePermissionByName(ctx, clientUUID, permissionName)
	if err != nil {
		c.logger.WarnContext(ctx, "Failed to delete permission (may not exist)",
			slog.String("permissionName", permissionName),
			slog.Any("error", err),
		)
	}

	// Delete the policy
	policyName := fmt.Sprintf("Group policy for %s - %s", resourceName, scopeName)
	err = c.deletePolicyByName(ctx, clientUUID, policyName)
	if err != nil {
		c.logger.WarnContext(ctx, "Failed to delete policy (may not exist)",
			slog.String("policyName", policyName),
			slog.Any("error", err),
		)
	}

	c.logger.InfoContext(ctx, "Deleted authorization group policy",
		slog.String("resourceName", resourceName),
		slog.String("scope", scopeName),
	)

	return nil
}

// Helper methods

// GetGroupIDByPath gets a Keycloak group ID by its path.
// This is exposed for use by the ResourceManager.
func (c *Client) GetGroupIDByPath(ctx context.Context, groupPath string) (string, error) {
	return c.getGroupIDByPath(ctx, groupPath)
}

func (c *Client) getGroupIDByPath(ctx context.Context, groupPath string) (string, error) {
	// Search for group by path
	// Note: Keycloak doesn't have a direct "get by path" API, so we search by name
	// and verify the path matches
	path := fmt.Sprintf("/admin/realms/%s/groups?search=%s",
		url.PathEscape(c.realmName),
		url.QueryEscape(groupPath),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("failed to search groups: %w", err)
	}
	defer response.Body.Close()

	var groups []struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(response.Body).Decode(&groups); err != nil {
		return "", fmt.Errorf("failed to decode groups: %w", err)
	}

	for _, group := range groups {
		if group.Path == groupPath {
			return group.ID, nil
		}
	}

	return "", fmt.Errorf("group not found: %s", groupPath)
}

func (c *Client) createGroupPolicy(ctx context.Context, clientUUID, groupPath, policyName string) (string, error) {
	// Check if policy already exists
	existingPolicyID, err := c.getPolicyByName(ctx, clientUUID, policyName)
	if err == nil && existingPolicyID != "" {
		c.logger.DebugContext(ctx, "Group policy already exists",
			slog.String("policyID", existingPolicyID),
		)
		return existingPolicyID, nil
	}

	// Create new group policy
	path := fmt.Sprintf("/admin/realms/%s/clients/%s/authz/resource-server/policy/group",
		url.PathEscape(c.realmName),
		url.PathEscape(clientUUID),
	)

	policyPayload := map[string]interface{}{
		"name":        policyName,
		"description": fmt.Sprintf("Grants group %s access to resource", groupPath),
		"groups": []map[string]interface{}{
			{
				"path": groupPath,
			},
		},
		"logic": "POSITIVE",
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, path, policyPayload)
	if err != nil {
		return "", fmt.Errorf("failed to create group policy: %w", err)
	}
	defer response.Body.Close()

	var policy struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&policy); err != nil {
		return "", fmt.Errorf("failed to decode policy response: %w", err)
	}

	return policy.ID, nil
}

func (c *Client) deletePolicyByName(ctx context.Context, clientUUID, policyName string) error {
	// Get the policy ID first
	policyID, err := c.getPolicyByName(ctx, clientUUID, policyName)
	if err != nil {
		return fmt.Errorf("policy not found: %w", err)
	}

	path := fmt.Sprintf("/admin/realms/%s/clients/%s/authz/resource-server/policy/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(clientUUID),
		url.PathEscape(policyID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete policy: %w", err)
	}
	defer response.Body.Close()

	return nil
}
