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

// ResourceManager handles Keycloak group operations for authorization.
// Works with any IdP client that implements group management.
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

// DeleteProjectGroups deletes Keycloak organization groups for a project.
func (m *ResourceManager) DeleteProjectGroups(ctx context.Context, tenant, projectName string) error {
	if tenant == "" {
		return fmt.Errorf("tenant is required")
	}
	if projectName == "" {
		return fmt.Errorf("project name is required")
	}

	// Delete the parent project group, which will cascade delete the viewers and managers subgroups
	projectGroupPath := fmt.Sprintf("/%s", projectName)

	projectGroupID, err := m.getGroupIDByPath(ctx, tenant, projectGroupPath)
	if err != nil {
		// Only swallow "not found" errors - propagate other errors (network, auth, etc.) for retry
		if strings.Contains(err.Error(), "organization group not found") {
			m.logger.WarnContext(ctx, "Project group not found, skipping deletion",
				slog.String("group_path", projectGroupPath),
				slog.String("tenant", tenant),
			)
			return nil
		}
		m.logger.ErrorContext(ctx, "Failed to get project group ID",
			slog.String("group_path", projectGroupPath),
			slog.String("tenant", tenant),
			slog.Any("error", err),
		)
		return fmt.Errorf("failed to get project group ID: %w", err)
	}

	if err = m.client.DeleteAuthorizationGroup(ctx, tenant, projectGroupID); err != nil {
		m.logger.ErrorContext(ctx, "Failed to delete project group",
			slog.String("group_id", projectGroupID),
			slog.String("group_path", projectGroupPath),
			slog.Any("error", err),
		)
		return fmt.Errorf("failed to delete project group %s: %w", projectGroupPath, err)
	}

	m.logger.InfoContext(ctx, "Deleted project group and subgroups",
		slog.String("group_path", projectGroupPath),
		slog.String("project_name", projectName),
		slog.String("tenant", tenant),
	)

	return nil
}

// getGroupIDByPath is a helper to get the group ID from a group path.
func (m *ResourceManager) getGroupIDByPath(ctx context.Context, organizationName, groupPath string) (string, error) {
	return m.client.GetGroupIDByPath(ctx, organizationName, groupPath)
}

// CreateProjectGroups creates Keycloak organization groups for a project.
// Creates hierarchical groups: /{project-name}/viewers and /{project-name}/managers
// These groups are used by Authorino OPA policies for authorization.
func (m *ResourceManager) CreateProjectGroups(ctx context.Context, tenant, projectName string) error {
	m.logger.DebugContext(ctx, "Creating project groups",
		slog.String("tenant", tenant),
		slog.String("project_name", projectName),
	)

	viewersGroupPath := fmt.Sprintf("/%s/%s", projectName, GroupNameViewers)
	err := m.client.CreateAuthorizationGroup(ctx, tenant, GroupNameViewers, viewersGroupPath)
	if err != nil {
		return fmt.Errorf("failed to create viewers group: %w", err)
	}

	m.logger.InfoContext(ctx, "Created project viewers group",
		slog.String("group_path", viewersGroupPath),
		slog.String("project_name", projectName),
		slog.String("tenant", tenant),
	)

	managersGroupPath := fmt.Sprintf("/%s/%s", projectName, GroupNameManagers)
	err = m.client.CreateAuthorizationGroup(ctx, tenant, GroupNameManagers, managersGroupPath)
	if err != nil {
		// Clean up viewers group on failure
		viewersGroupID, getErr := m.getGroupIDByPath(ctx, tenant, viewersGroupPath)
		if getErr != nil {
			m.logger.ErrorContext(ctx, "Failed to get viewers group ID during rollback",
				slog.String("group_path", viewersGroupPath),
				slog.Any("get_error", getErr),
			)
			return fmt.Errorf("failed to create managers group: %w (rollback also failed to lookup viewers group: %w)", err, getErr)
		}
		if viewersGroupID != "" {
			if cleanupErr := m.client.DeleteAuthorizationGroup(ctx, tenant, viewersGroupID); cleanupErr != nil {
				m.logger.ErrorContext(ctx, "Failed to cleanup viewers group during rollback",
					slog.String("group_id", viewersGroupID),
					slog.Any("cleanup_error", cleanupErr),
				)
				return fmt.Errorf("failed to create managers group: %w (rollback also failed: %w)", err, cleanupErr)
			}
		}
		return fmt.Errorf("failed to create managers group: %w", err)
	}

	m.logger.InfoContext(ctx, "Created project managers group",
		slog.String("group_path", managersGroupPath),
		slog.String("project_name", projectName),
		slog.String("tenant", tenant),
	)

	return nil
}
