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

	return createdResource.ID, nil
}

// DeleteAuthorizationResource deletes an Authorization Resource by ID.
func (m *ResourceManager) DeleteAuthorizationResource(ctx context.Context, resourceID string) error {
	m.logger.DebugContext(ctx, "Deleting authorization resource",
		slog.String("resource_id", resourceID),
	)

	err := m.client.DeleteAuthorizationResource(ctx, resourceID)
	if err != nil {
		return fmt.Errorf("failed to delete authorization resource: %w", err)
	}

	m.logger.InfoContext(ctx, "Authorization resource deleted",
		slog.String("resource_id", resourceID),
	)

	return nil
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
