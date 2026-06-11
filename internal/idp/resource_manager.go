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
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// ResourceManager handles authorization resource operations.
// Works with any IdP client that implements authorization resources.
type ResourceManager struct {
	logger *slog.Logger
	client Client
}

// ResourceManagerBuilder builds the resource manager.
type ResourceManagerBuilder struct {
	logger *slog.Logger
	client Client
}

// NewResourceManager creates a builder for the resource manager.
func NewResourceManager() *ResourceManagerBuilder {
	return &ResourceManagerBuilder{}
}

// SetLogger sets the logger.
func (b *ResourceManagerBuilder) SetLogger(value *slog.Logger) *ResourceManagerBuilder {
	b.logger = value
	return b
}

// SetClient sets the IdP client implementation.
func (b *ResourceManagerBuilder) SetClient(value Client) *ResourceManagerBuilder {
	b.client = value
	return b
}

// Build creates the manager.
func (b *ResourceManagerBuilder) Build() (result *ResourceManager, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.client == nil {
		err = errors.New("IdP client is mandatory")
		return
	}

	result = &ResourceManager{
		logger: b.logger,
		client: b.client,
	}
	return
}

// CreateProjectAuthorizationResource creates an Authorization Resource for a project.
// The resource name follows the format: PROJECT-{tenant}-{projectName}
// This also creates authorization groups for viewer and manager access.
func (m *ResourceManager) CreateProjectAuthorizationResource(ctx context.Context, projectID, tenant, projectName string, scopes []string) (string, error) {
	resourceName := fmt.Sprintf("PROJECT-%s-%s", tenant, projectName)

	resource := &AuthorizationResource{
		Name:   resourceName,
		Scopes: scopes,
		Attributes: map[string][]string{
			"project_id": {projectID},
			"tenant":     {tenant},
		},
	}

	m.logger.DebugContext(ctx, "Creating project authorization resource",
		slog.String("resource_name", resourceName),
		slog.String("project_id", projectID),
	)

	createdResource, err := m.client.CreateAuthorizationResource(ctx, resource)
	if err != nil {
		return "", fmt.Errorf("failed to create authorization resource: %w", err)
	}
	if createdResource == nil {
		return "", fmt.Errorf("created authorization resource is nil")
	}
	if createdResource.ID == "" {
		return "", fmt.Errorf("created authorization resource has empty ID")
	}

	m.logger.InfoContext(ctx, "Project authorization resource created",
		slog.String("resource_id", createdResource.ID),
		slog.String("resource_name", createdResource.Name),
		slog.String("project_id", projectID),
	)

	// Create authorization groups for the project
	err = m.createProjectAuthorizationGroups(ctx, createdResource.ID, tenant, projectName)
	if err != nil {
		// Clean up the resource if group creation fails
		m.logger.ErrorContext(ctx, "Failed to create authorization groups, cleaning up resource",
			slog.String("resource_id", createdResource.ID),
			slog.Any("error", err),
		)
		if cleanupErr := m.client.DeleteAuthorizationResource(ctx, createdResource.ID); cleanupErr != nil {
			m.logger.ErrorContext(ctx, "Failed to cleanup authorization resource during rollback",
				slog.String("resource_id", createdResource.ID),
				slog.Any("cleanup_error", cleanupErr),
			)
			return "", fmt.Errorf("failed to create authorization groups: %w (cleanup also failed: %w)", err, cleanupErr)
		}
		return "", fmt.Errorf("failed to create authorization groups: %w", err)
	}

	return createdResource.ID, nil
}

// createProjectAuthorizationGroups creates Keycloak organization groups
// for viewer and manager access to a project.
//
// Uses the hierarchical naming convention:
//   - Top-level: /{project-name}/{viewers|managers}
//   - Nested: /{parent-project}/{sub-project}/{viewers|managers}
//
// The projectName parameter should contain the full project path (e.g., "web-app" or "web-app/api").
func (m *ResourceManager) createProjectAuthorizationGroups(ctx context.Context, resourceID, organizationName, projectName string) error {
	resourceName := fmt.Sprintf("PROJECT-%s-%s", organizationName, projectName)

	// Create viewers group using hierarchical path
	// Group path: /{project-name}/{viewers}
	// The CreateAuthorizationGroup method will create parent group (/{project-name})
	// if it doesn't exist, then create the viewers group under it
	viewersGroupPath := fmt.Sprintf("/%s/%s", projectName, GroupNameViewers)
	err := m.client.CreateAuthorizationGroup(ctx, organizationName, GroupNameViewers, viewersGroupPath)
	if err != nil {
		return fmt.Errorf("failed to create viewers group: %w", err)
	}

	m.logger.InfoContext(ctx, "Created project viewers group",
		slog.String("group_path", viewersGroupPath),
		slog.String("project_name", resourceName),
		slog.String("organization", organizationName),
	)

	// Create managers group using hierarchical path
	managersGroupPath := fmt.Sprintf("/%s/%s", projectName, GroupNameManagers)
	err = m.client.CreateAuthorizationGroup(ctx, organizationName, GroupNameManagers, managersGroupPath)
	if err != nil {
		// Clean up viewers group on failure
		viewersGroupID, getErr := m.getGroupIDByPath(ctx, organizationName, viewersGroupPath)
		if getErr != nil {
			m.logger.ErrorContext(ctx, "Failed to get viewers group ID during rollback",
				slog.String("group_path", viewersGroupPath),
				slog.Any("get_error", getErr),
			)
			// Return composite error including lookup failure
			return fmt.Errorf("failed to create managers group: %w (rollback also failed to lookup viewers group: %w)", err, getErr)
		}
		if viewersGroupID != "" {
			if cleanupErr := m.client.DeleteAuthorizationGroup(ctx, organizationName, viewersGroupID); cleanupErr != nil {
				m.logger.ErrorContext(ctx, "Failed to cleanup viewers group during rollback",
					slog.String("group_id", viewersGroupID),
					slog.String("group_path", viewersGroupPath),
					slog.Any("cleanup_error", cleanupErr),
				)
				return fmt.Errorf("failed to create managers group: %w (rollback also failed to delete viewers group: %w)", err, cleanupErr)
			}
		}
		return fmt.Errorf("failed to create managers group: %w", err)
	}

	m.logger.InfoContext(ctx, "Created project managers group",
		slog.String("group_path", managersGroupPath),
		slog.String("project_name", resourceName),
		slog.String("organization", organizationName),
	)

	// Create group policy for viewers
	viewersPolicyName := fmt.Sprintf("%s-viewers-policy", projectName)
	viewersPolicy := &AuthorizationPolicy{
		Name:   viewersPolicyName,
		Type:   "group",
		Logic:  "POSITIVE",
		Groups: []string{viewersGroupPath},
	}

	createdViewersPolicy, err := m.client.CreateGroupPolicy(ctx, viewersPolicy)
	if err != nil {
		// Clean up groups on failure
		m.cleanupGroupsOnFailure(ctx, organizationName, viewersGroupPath, managersGroupPath)
		return fmt.Errorf("failed to create viewers policy: %w", err)
	}

	m.logger.InfoContext(ctx, "Created viewers authorization policy",
		slog.String("policy_id", createdViewersPolicy.ID),
		slog.String("policy_name", createdViewersPolicy.Name),
		slog.String("project_name", resourceName),
	)

	// Create group policy for managers
	managersPolicyName := fmt.Sprintf("%s-managers-policy", projectName)
	managersPolicy := &AuthorizationPolicy{
		Name:   managersPolicyName,
		Type:   "group",
		Logic:  "POSITIVE",
		Groups: []string{managersGroupPath},
	}

	createdManagersPolicy, err := m.client.CreateGroupPolicy(ctx, managersPolicy)
	if err != nil {
		// Clean up groups and viewers policy on failure
		_ = m.client.DeletePolicy(ctx, createdViewersPolicy.ID)
		m.cleanupGroupsOnFailure(ctx, organizationName, viewersGroupPath, managersGroupPath)
		return fmt.Errorf("failed to create managers policy: %w", err)
	}

	m.logger.InfoContext(ctx, "Created managers authorization policy",
		slog.String("policy_id", createdManagersPolicy.ID),
		slog.String("policy_name", createdManagersPolicy.Name),
		slog.String("project_name", resourceName),
	)

	// Create scope permission for viewers (VIEW_PROJECT scope)
	viewersPermissionName := fmt.Sprintf("%s-view-permission", projectName)
	viewersPermission := &AuthorizationPermission{
		Name:             viewersPermissionName,
		Type:             "scope",
		Logic:            "POSITIVE",
		DecisionStrategy: "UNANIMOUS",
		ResourceID:       resourceID,
		Scopes:           []string{ScopeViewProject},
		Policies:         []string{createdViewersPolicy.ID},
	}

	createdViewersPermission, err := m.client.CreateScopePermission(ctx, viewersPermission)
	if err != nil {
		// Clean up policies and groups on failure
		_ = m.client.DeletePolicy(ctx, createdManagersPolicy.ID)
		_ = m.client.DeletePolicy(ctx, createdViewersPolicy.ID)
		m.cleanupGroupsOnFailure(ctx, organizationName, viewersGroupPath, managersGroupPath)
		return fmt.Errorf("failed to create viewers permission: %w", err)
	}

	m.logger.InfoContext(ctx, "Created viewers scope permission",
		slog.String("permission_id", createdViewersPermission.ID),
		slog.String("permission_name", createdViewersPermission.Name),
		slog.String("project_name", resourceName),
	)

	// Create scope permission for managers (MANAGE_PROJECT scope)
	managersPermissionName := fmt.Sprintf("%s-manage-permission", projectName)
	managersPermission := &AuthorizationPermission{
		Name:             managersPermissionName,
		Type:             "scope",
		Logic:            "POSITIVE",
		DecisionStrategy: "UNANIMOUS",
		ResourceID:       resourceID,
		Scopes:           []string{ScopeManageProject},
		Policies:         []string{createdManagersPolicy.ID},
	}

	createdManagersPermission, err := m.client.CreateScopePermission(ctx, managersPermission)
	if err != nil {
		// Clean up viewers permission, policies, and groups on failure
		_ = m.client.DeletePermission(ctx, createdViewersPermission.ID)
		_ = m.client.DeletePolicy(ctx, createdManagersPolicy.ID)
		_ = m.client.DeletePolicy(ctx, createdViewersPolicy.ID)
		m.cleanupGroupsOnFailure(ctx, organizationName, viewersGroupPath, managersGroupPath)
		return fmt.Errorf("failed to create managers permission: %w", err)
	}

	m.logger.InfoContext(ctx, "Created managers scope permission",
		slog.String("permission_id", createdManagersPermission.ID),
		slog.String("permission_name", createdManagersPermission.Name),
		slog.String("project_name", resourceName),
	)

	return nil
}

// cleanupGroupsOnFailure is a helper to clean up groups when policy or permission creation fails.
func (m *ResourceManager) cleanupGroupsOnFailure(ctx context.Context, organizationName, viewersGroupPath, managersGroupPath string) {
	viewersGroupID, _ := m.getGroupIDByPath(ctx, organizationName, viewersGroupPath)
	if viewersGroupID != "" {
		_ = m.client.DeleteAuthorizationGroup(ctx, organizationName, viewersGroupID)
	}

	managersGroupID, _ := m.getGroupIDByPath(ctx, organizationName, managersGroupPath)
	if managersGroupID != "" {
		_ = m.client.DeleteAuthorizationGroup(ctx, organizationName, managersGroupID)
	}
}

// DeleteAuthorizationResource deletes an Authorization Resource by ID.
// This also deletes the associated groups
func (m *ResourceManager) DeleteAuthorizationResource(ctx context.Context, resourceID string) error {
	m.logger.DebugContext(ctx, "Deleting authorization resource",
		slog.String("resource_id", resourceID),
	)

	// Get the resource to extract tenant and project name for group cleanup
	resource, err := m.client.GetAuthorizationResource(ctx, resourceID)
	if err != nil {
		// Resource might already be deleted, log and continue
		m.logger.WarnContext(ctx, "Failed to get authorization resource for cleanup",
			slog.String("resource_id", resourceID),
			slog.Any("error", err),
		)
	} else {
		// Extract organization name from resource attributes
		organizationName := ""
		if tenants, ok := resource.Attributes["tenant"]; ok && len(tenants) > 0 {
			organizationName = tenants[0]
		}

		// Delete groups
		err = m.deleteProjectAuthorizationGroups(ctx, resource.Name, organizationName)
		if err != nil {
			m.logger.WarnContext(ctx, "Failed to delete authorization groups",
				slog.String("resource_name", resource.Name),
				slog.String("organization", organizationName),
				slog.Any("error", err),
			)
			// Continue with resource deletion even if group cleanup fails
		}
	}

	// Delete the resource
	err = m.client.DeleteAuthorizationResource(ctx, resourceID)
	if err != nil {
		return fmt.Errorf("failed to delete authorization resource: %w", err)
	}

	m.logger.InfoContext(ctx, "Authorization resource deleted",
		slog.String("resource_id", resourceID),
	)

	return nil
}

// deleteProjectAuthorizationGroups deletes Keycloak organization groups,
// policies, and permissions for a project resource.
func (m *ResourceManager) deleteProjectAuthorizationGroups(ctx context.Context, resourceName, organizationName string) error {
	if organizationName == "" {
		return fmt.Errorf("organization name is required for deleting groups")
	}

	// Extract project name from resource name (format: PROJECT-{tenant}-{name})
	resourcePrefix := "PROJECT-"
	if !strings.HasPrefix(resourceName, resourcePrefix) {
		m.logger.WarnContext(ctx, "Unexpected resource name format, skipping group deletion",
			slog.String("resource_name", resourceName),
		)
		return nil
	}
	// Remove "PROJECT-{tenant}-" prefix to get just the project name
	parts := resourceName[len(resourcePrefix):]
	// Find the first '-' to split tenant from project name
	firstDash := len(organizationName)
	if len(parts) > firstDash+1 {
		projectName := parts[firstDash+1:] // Skip tenant and dash

		// Use new hierarchical paths: /{project-name}/{viewers|managers}
		viewersGroupPath := fmt.Sprintf("/%s/%s", projectName, GroupNameViewers)
		managersGroupPath := fmt.Sprintf("/%s/%s", projectName, GroupNameManagers)

		// Delete permissions first (they reference policies)
		viewersPermissionName := fmt.Sprintf("%s-view-permission", projectName)
		viewersPermissionID, err := m.getPermissionIDByName(ctx, viewersPermissionName)
		if err != nil {
			m.logger.WarnContext(ctx, "Failed to get viewers permission ID for deletion",
				slog.String("permission_name", viewersPermissionName),
				slog.Any("error", err),
			)
		} else {
			err = m.client.DeletePermission(ctx, viewersPermissionID)
			if err != nil {
				m.logger.WarnContext(ctx, "Failed to delete viewers permission",
					slog.String("permission_id", viewersPermissionID),
					slog.Any("error", err),
				)
			}
		}

		managersPermissionName := fmt.Sprintf("%s-manage-permission", projectName)
		managersPermissionID, err := m.getPermissionIDByName(ctx, managersPermissionName)
		if err != nil {
			m.logger.WarnContext(ctx, "Failed to get managers permission ID for deletion",
				slog.String("permission_name", managersPermissionName),
				slog.Any("error", err),
			)
		} else {
			err = m.client.DeletePermission(ctx, managersPermissionID)
			if err != nil {
				m.logger.WarnContext(ctx, "Failed to delete managers permission",
					slog.String("permission_id", managersPermissionID),
					slog.Any("error", err),
				)
			}
		}

		// Delete policies (they reference groups)
		viewersPolicyName := fmt.Sprintf("%s-viewers-policy", projectName)
		viewersPolicyID, err := m.getPolicyIDByName(ctx, viewersPolicyName)
		if err != nil {
			m.logger.WarnContext(ctx, "Failed to get viewers policy ID for deletion",
				slog.String("policy_name", viewersPolicyName),
				slog.Any("error", err),
			)
		} else {
			err = m.client.DeletePolicy(ctx, viewersPolicyID)
			if err != nil {
				m.logger.WarnContext(ctx, "Failed to delete viewers policy",
					slog.String("policy_id", viewersPolicyID),
					slog.Any("error", err),
				)
			}
		}

		managersPolicyName := fmt.Sprintf("%s-managers-policy", projectName)
		managersPolicyID, err := m.getPolicyIDByName(ctx, managersPolicyName)
		if err != nil {
			m.logger.WarnContext(ctx, "Failed to get managers policy ID for deletion",
				slog.String("policy_name", managersPolicyName),
				slog.Any("error", err),
			)
		} else {
			err = m.client.DeletePolicy(ctx, managersPolicyID)
			if err != nil {
				m.logger.WarnContext(ctx, "Failed to delete managers policy",
					slog.String("policy_id", managersPolicyID),
					slog.Any("error", err),
				)
			}
		}

		// Finally, delete groups
		viewersGroupID, err := m.getGroupIDByPath(ctx, organizationName, viewersGroupPath)
		if err != nil {
			m.logger.WarnContext(ctx, "Failed to get viewers group ID for deletion",
				slog.String("group_path", viewersGroupPath),
				slog.String("organization", organizationName),
				slog.Any("error", err),
			)
		} else {
			err = m.client.DeleteAuthorizationGroup(ctx, organizationName, viewersGroupID)
			if err != nil {
				m.logger.WarnContext(ctx, "Failed to delete viewers group",
					slog.String("group_id", viewersGroupID),
					slog.String("organization", organizationName),
					slog.Any("error", err),
				)
			}
		}

		managersGroupID, err := m.getGroupIDByPath(ctx, organizationName, managersGroupPath)
		if err != nil {
			m.logger.WarnContext(ctx, "Failed to get managers group ID for deletion",
				slog.String("group_path", managersGroupPath),
				slog.String("organization", organizationName),
				slog.Any("error", err),
			)
		} else {
			err = m.client.DeleteAuthorizationGroup(ctx, organizationName, managersGroupID)
			if err != nil {
				m.logger.WarnContext(ctx, "Failed to delete managers group",
					slog.String("group_id", managersGroupID),
					slog.String("organization", organizationName),
					slog.Any("error", err),
				)
			}
		}
	}

	return nil
}

// getGroupIDByPath is a helper to get the group ID from a group path.
// This delegates to the client's GetGroupIDByPath implementation.
func (m *ResourceManager) getGroupIDByPath(ctx context.Context, organizationName, groupPath string) (string, error) {
	return m.client.GetGroupIDByPath(ctx, organizationName, groupPath)
}

// getPolicyIDByName is a helper to get the policy ID from a policy name.
// This is a Keycloak-specific operation and may not be available on all IdP clients.
func (m *ResourceManager) getPolicyIDByName(ctx context.Context, policyName string) (string, error) {
	// This relies on the Keycloak client implementation
	// If the client doesn't support this, it will return an error
	type policyIDGetter interface {
		GetPolicyIDByName(ctx context.Context, policyName string) (string, error)
	}

	if getter, ok := m.client.(policyIDGetter); ok {
		return getter.GetPolicyIDByName(ctx, policyName)
	}

	return "", fmt.Errorf("client does not support getting policy ID by name")
}

// getPermissionIDByName is a helper to get the permission ID from a permission name.
// This is a Keycloak-specific operation and may not be available on all IdP clients.
func (m *ResourceManager) getPermissionIDByName(ctx context.Context, permissionName string) (string, error) {
	// This relies on the Keycloak client implementation
	// If the client doesn't support this, it will return an error
	type permissionIDGetter interface {
		GetPermissionIDByName(ctx context.Context, permissionName string) (string, error)
	}

	if getter, ok := m.client.(permissionIDGetter); ok {
		return getter.GetPermissionIDByName(ctx, permissionName)
	}

	return "", fmt.Errorf("client does not support getting permission ID by name")
}

// GetAuthorizationResource retrieves an Authorization Resource by ID.
func (m *ResourceManager) GetAuthorizationResource(ctx context.Context, resourceID string) (*AuthorizationResource, error) {
	m.logger.DebugContext(ctx, "Getting authorization resource",
		slog.String("resource_id", resourceID),
	)

	resource, err := m.client.GetAuthorizationResource(ctx, resourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get authorization resource: %w", err)
	}

	return resource, nil
}
