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
// This also creates authorization groups and policies for viewer and manager access.
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

	// Create authorization groups and policies for the project
	err = m.createProjectAuthorizationGroupsAndPolicies(ctx, createdResource.ID, tenant, projectName)
	if err != nil {
		// Clean up the resource if group/policy creation fails
		m.logger.ErrorContext(ctx, "Failed to create authorization groups/policies, cleaning up resource",
			slog.String("resource_id", createdResource.ID),
			slog.Any("error", err),
		)
		_ = m.client.DeleteAuthorizationResource(ctx, createdResource.ID)
		return "", fmt.Errorf("failed to create authorization groups and policies: %w", err)
	}

	return createdResource.ID, nil
}

// createProjectAuthorizationGroupsAndPolicies creates Keycloak groups and group-based policies
// for viewer and manager access to a project.
func (m *ResourceManager) createProjectAuthorizationGroupsAndPolicies(ctx context.Context, resourceID, tenant, projectName string) error {
	resourceName := fmt.Sprintf("PROJECT-%s-%s", tenant, projectName)
	baseGroupName := fmt.Sprintf("project-%s-%s", tenant, projectName)

	// Create viewers group
	viewersGroupName := fmt.Sprintf("%s-viewers", baseGroupName)
	viewersGroupPath := fmt.Sprintf("/%s", viewersGroupName)
	err := m.client.CreateAuthorizationGroup(ctx, viewersGroupName, viewersGroupPath)
	if err != nil {
		return fmt.Errorf("failed to create viewers group: %w", err)
	}

	m.logger.InfoContext(ctx, "Created project viewers group",
		slog.String("group_name", viewersGroupName),
		slog.String("project_name", resourceName),
	)

	// Create managers group
	managersGroupName := fmt.Sprintf("%s-managers", baseGroupName)
	managersGroupPath := fmt.Sprintf("/%s", managersGroupName)
	err = m.client.CreateAuthorizationGroup(ctx, managersGroupName, managersGroupPath)
	if err != nil {
		// Clean up viewers group on failure
		_ = m.client.DeleteAuthorizationGroup(ctx, viewersGroupPath)
		return fmt.Errorf("failed to create managers group: %w", err)
	}

	m.logger.InfoContext(ctx, "Created project managers group",
		slog.String("group_name", managersGroupName),
		slog.String("project_name", resourceName),
	)

	// Create group policy and permission for viewers (VIEW_PROJECT scope)
	err = m.client.CreateAuthorizationGroupPolicy(ctx, resourceID, resourceName, viewersGroupPath, ScopeViewProject)
	if err != nil {
		// Clean up groups on failure
		_ = m.client.DeleteAuthorizationGroup(ctx, viewersGroupPath)
		_ = m.client.DeleteAuthorizationGroup(ctx, managersGroupPath)
		return fmt.Errorf("failed to create viewers policy: %w", err)
	}

	m.logger.InfoContext(ctx, "Created project viewers policy",
		slog.String("resource_name", resourceName),
		slog.String("scope", ScopeViewProject),
	)

	// Create group policy and permission for managers (MANAGE_PROJECT scope)
	err = m.client.CreateAuthorizationGroupPolicy(ctx, resourceID, resourceName, managersGroupPath, ScopeManageProject)
	if err != nil {
		// Clean up groups and viewers policy on failure
		_ = m.client.DeleteAuthorizationGroupPolicy(ctx, resourceName, ScopeViewProject)
		_ = m.client.DeleteAuthorizationGroup(ctx, viewersGroupPath)
		_ = m.client.DeleteAuthorizationGroup(ctx, managersGroupPath)
		return fmt.Errorf("failed to create managers policy: %w", err)
	}

	m.logger.InfoContext(ctx, "Created project managers policy",
		slog.String("resource_name", resourceName),
		slog.String("scope", ScopeManageProject),
	)

	return nil
}

// DeleteAuthorizationResource deletes an Authorization Resource by ID.
// This also deletes the associated groups and policies.
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
		// Delete groups and policies
		err = m.deleteProjectAuthorizationGroupsAndPolicies(ctx, resource.Name)
		if err != nil {
			m.logger.WarnContext(ctx, "Failed to delete authorization groups/policies",
				slog.String("resource_name", resource.Name),
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

// deleteProjectAuthorizationGroupsAndPolicies deletes Keycloak groups and group-based policies
// for a project resource.
func (m *ResourceManager) deleteProjectAuthorizationGroupsAndPolicies(ctx context.Context, resourceName string) error {
	// Extract tenant and project name from resource name (format: PROJECT-{tenant}-{name})
	// For now, we'll derive the group names directly from the resource name
	baseGroupName := resourceName[len("PROJECT-"):] // Remove "PROJECT-" prefix to get "{tenant}-{name}"

	viewersGroupName := fmt.Sprintf("project-%s-viewers", baseGroupName)
	viewersGroupPath := fmt.Sprintf("/%s", viewersGroupName)
	managersGroupName := fmt.Sprintf("project-%s-managers", baseGroupName)
	managersGroupPath := fmt.Sprintf("/%s", managersGroupName)

	// Delete policies first (they reference the groups)
	err := m.client.DeleteAuthorizationGroupPolicy(ctx, resourceName, ScopeViewProject)
	if err != nil {
		m.logger.WarnContext(ctx, "Failed to delete viewers policy",
			slog.String("resource_name", resourceName),
			slog.Any("error", err),
		)
	}

	err = m.client.DeleteAuthorizationGroupPolicy(ctx, resourceName, ScopeManageProject)
	if err != nil {
		m.logger.WarnContext(ctx, "Failed to delete managers policy",
			slog.String("resource_name", resourceName),
			slog.Any("error", err),
		)
	}

	// Get group IDs and delete groups
	viewersGroupID, err := m.getGroupIDByPath(ctx, viewersGroupPath)
	if err != nil {
		m.logger.WarnContext(ctx, "Failed to get viewers group ID for deletion",
			slog.String("group_path", viewersGroupPath),
			slog.Any("error", err),
		)
	} else {
		err = m.client.DeleteAuthorizationGroup(ctx, viewersGroupID)
		if err != nil {
			m.logger.WarnContext(ctx, "Failed to delete viewers group",
				slog.String("group_id", viewersGroupID),
				slog.Any("error", err),
			)
		}
	}

	managersGroupID, err := m.getGroupIDByPath(ctx, managersGroupPath)
	if err != nil {
		m.logger.WarnContext(ctx, "Failed to get managers group ID for deletion",
			slog.String("group_path", managersGroupPath),
			slog.Any("error", err),
		)
	} else {
		err = m.client.DeleteAuthorizationGroup(ctx, managersGroupID)
		if err != nil {
			m.logger.WarnContext(ctx, "Failed to delete managers group",
				slog.String("group_id", managersGroupID),
				slog.Any("error", err),
			)
		}
	}

	return nil
}

// getGroupIDByPath is a helper to get the group ID from a group path.
// This delegates to the Keycloak client's internal implementation.
func (m *ResourceManager) getGroupIDByPath(ctx context.Context, groupPath string) (string, error) {
	// We need to expose a method on the client to get group ID by path
	// For now, we'll use a workaround via the authz group interface
	type groupIDGetter interface {
		GetGroupIDByPath(ctx context.Context, groupPath string) (string, error)
	}

	if getter, ok := m.client.(groupIDGetter); ok {
		return getter.GetGroupIDByPath(ctx, groupPath)
	}

	return "", fmt.Errorf("client does not support GetGroupIDByPath")
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
