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
	"strings"
)

// CreateAuthorizationGroup creates a Keycloak organization group for authorization purposes.
// Organization groups are scoped to the organization and support hierarchical paths.
//
// Group path format examples:
//   - Top-level project: "/{project-name}/{viewers|managers}"
//     Example: "/web-app/viewers"
//   - Nested project: "/{parent-project}/{sub-project}/{viewers|managers}"
//     Example: "/web-app/api/viewers"
//   - Deeper nesting: "/{project}/{sub-project}/{component}/{viewers|managers}"
//     Example: "/platform/web-app/api/viewers"
//
// Organization groups are scoped per organization, so paths can be simple and readable.
// This method creates the full hierarchy if parent groups don't exist.
// See https://www.keycloak.org/2026/04/org-groups for details.
func (c *Client) CreateAuthorizationGroup(ctx context.Context, organizationName, groupName, groupPath string) error {
	c.logger.DebugContext(ctx, "Creating organization authorization group",
		slog.String("organizationName", organizationName),
		slog.String("groupName", groupName),
		slog.String("groupPath", groupPath),
	)

	// Get the organization ID first
	org, err := c.GetOrganization(ctx, organizationName)
	if err != nil {
		return fmt.Errorf("failed to get organization: %w", err)
	}

	// Parse the path to create parent groups if needed
	// Path format: /web-app/viewers
	// We need to ensure /web-app exists, then create viewers under it
	// Use a cache to avoid redundant API calls within the same operation
	cache := make(map[string]string) // path -> groupID
	err = c.ensureGroupHierarchyWithCache(ctx, org.ID, groupPath, cache)
	if err != nil {
		return fmt.Errorf("failed to ensure group hierarchy: %w", err)
	}

	c.logger.DebugContext(ctx, "Created organization authorization group",
		slog.String("organizationName", organizationName),
		slog.String("groupName", groupName),
		slog.String("groupPath", groupPath),
	)

	return nil
}

// DeleteAuthorizationGroup deletes a Keycloak organization group by ID.
func (c *Client) DeleteAuthorizationGroup(ctx context.Context, organizationName, groupID string) error {
	c.logger.DebugContext(ctx, "Deleting organization authorization group",
		slog.String("organizationName", organizationName),
		slog.String("groupID", groupID),
	)

	// Get the organization ID first
	org, err := c.GetOrganization(ctx, organizationName)
	if err != nil {
		return fmt.Errorf("failed to get organization: %w", err)
	}

	// Use organization groups API instead of realm groups
	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/groups/%s",
		url.PathEscape(c.realmName),
		url.PathEscape(org.ID),
		url.PathEscape(groupID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete organization group: %w", err)
	}
	defer response.Body.Close()

	c.logger.DebugContext(ctx, "Deleted organization authorization group",
		slog.String("organizationName", organizationName),
		slog.String("groupID", groupID),
	)

	return nil
}

// Helper methods

func (c *Client) ensureGroupHierarchyWithCache(ctx context.Context, orgID, groupPath string, cache map[string]string) error {
	// Split path into segments (removing leading slash)
	// "/web-app/viewers" -> ["web-app", "viewers"]
	segments := strings.Split(strings.Trim(groupPath, "/"), "/")
	if len(segments) == 0 {
		return fmt.Errorf("invalid group path: %s", groupPath)
	}

	var currentPath string
	var parentID string

	for _, segment := range segments {
		// Build the current path
		currentPath = currentPath + "/" + segment

		// Check cache first
		if cachedID, exists := cache[currentPath]; exists {
			parentID = cachedID
			continue
		}

		// Try to create the group - if it already exists (409), just look it up
		groupID, err := c.createOrganizationGroupWithParent(ctx, orgID, segment, parentID)
		if err != nil {
			// Check if it's a "already exists" error (409 conflict)
			if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "status=409") {
				// Group already exists, look up its ID using orgID directly
				groupID, lookupErr := c.getGroupIDByPathWithOrgID(ctx, orgID, currentPath)
				if lookupErr != nil {
					return fmt.Errorf("group %s already exists but failed to look up ID: %w", currentPath, lookupErr)
				}
				// Cache it for subsequent use
				cache[currentPath] = groupID
				parentID = groupID
				continue
			}
			return fmt.Errorf("failed to create group %s: %w", currentPath, err)
		}

		// Cache the created group
		cache[currentPath] = groupID
		// Use this group as parent for next iteration
		parentID = groupID
	}

	return nil
}

// createOrganizationGroupWithParent creates a group under a specific parent.
// If parentID is empty, creates a top-level group.
func (c *Client) createOrganizationGroupWithParent(ctx context.Context, orgID, name, parentID string) (string, error) {
	var path string
	if parentID == "" {
		// Create top-level group
		path = fmt.Sprintf("/admin/realms/%s/organizations/%s/groups",
			url.PathEscape(c.realmName),
			url.PathEscape(orgID),
		)
	} else {
		// Create child group under parent
		path = fmt.Sprintf("/admin/realms/%s/organizations/%s/groups/%s/children",
			url.PathEscape(c.realmName),
			url.PathEscape(orgID),
			url.PathEscape(parentID),
		)
	}

	groupPayload := map[string]interface{}{
		"name": name,
	}

	response, err := c.httpClient.DoRequest(ctx, http.MethodPost, path, groupPayload)
	if err != nil {
		return "", fmt.Errorf("failed to create organization group: %w", err)
	}
	defer response.Body.Close()

	// Extract the created group ID from the Location header
	location := response.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("no Location header in create group response")
	}

	// Location format: .../groups/{group-id}
	parts := strings.Split(location, "/")
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid Location header: %s", location)
	}
	groupID := parts[len(parts)-1]

	return groupID, nil
}

// groupNode represents a group in the hierarchy for recursive traversal
type groupNode struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Path      string      `json:"path"`
	SubGroups []groupNode `json:"subGroups"`
}

// getGroupIDByPathWithOrgID returns the group ID for a path using orgID directly (not organization name).
// This is used internally when we already have the orgID to avoid an extra lookup.
func (c *Client) getGroupIDByPathWithOrgID(ctx context.Context, orgID, groupPath string) (string, error) {
	path := fmt.Sprintf("/admin/realms/%s/organizations/%s/groups",
		url.PathEscape(c.realmName),
		url.PathEscape(orgID),
	)

	response, err := c.httpClient.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("failed to list organization groups: %w", err)
	}
	defer response.Body.Close()

	var groups []groupNode
	if err := json.NewDecoder(response.Body).Decode(&groups); err != nil {
		return "", fmt.Errorf("failed to decode organization groups: %w", err)
	}

	// Search recursively through the group hierarchy
	for _, group := range groups {
		if id := searchGroupRecursively(group, groupPath); id != "" {
			return id, nil
		}
	}

	c.logger.WarnContext(ctx, "Group not found in listed groups",
		slog.String("target_path", groupPath),
		slog.Int("total_groups", len(groups)),
	)
	return "", fmt.Errorf("organization group not found: %s", groupPath)
}

// searchGroupRecursively searches for a group by path in the group hierarchy.
func searchGroupRecursively(group groupNode, targetPath string) string {
	if group.Path == targetPath {
		return group.ID
	}
	for _, subGroup := range group.SubGroups {
		if id := searchGroupRecursively(subGroup, targetPath); id != "" {
			return id
		}
	}
	return ""
}

// GetGroupIDByPath gets a Keycloak organization group ID by its path.
// This is exposed for use by the ResourceManager.
func (c *Client) GetGroupIDByPath(ctx context.Context, organizationName, groupPath string) (string, error) {
	return c.getGroupIDByPath(ctx, organizationName, groupPath)
}

func (c *Client) getGroupIDByPath(ctx context.Context, organizationName, groupPath string) (string, error) {
	// Get the organization ID first
	org, err := c.GetOrganization(ctx, organizationName)
	if err != nil {
		return "", fmt.Errorf("failed to get organization: %w", err)
	}

	// Use getGroupIDByPathWithOrgID which lists all groups instead of using the search parameter.
	// The search parameter is unreliable for recently-created groups.
	return c.getGroupIDByPathWithOrgID(ctx, org.ID, groupPath)
}
