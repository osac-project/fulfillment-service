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
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
	"time"
)

// OrganizationManager handles the lifecycle of IdP organizations.
// It works with any IdP client implementation.
type OrganizationManager struct {
	logger *slog.Logger
	client Client
}

// OrganizationManagerBuilder builds the manager.
type OrganizationManagerBuilder struct {
	logger *slog.Logger
	client Client
}

// NewOrganizationManager creates a builder for the organization manager.
func NewOrganizationManager() *OrganizationManagerBuilder {
	return &OrganizationManagerBuilder{}
}

// SetLogger sets the logger.
func (b *OrganizationManagerBuilder) SetLogger(value *slog.Logger) *OrganizationManagerBuilder {
	b.logger = value
	return b
}

// SetClient sets the IdP client implementation.
func (b *OrganizationManagerBuilder) SetClient(value Client) *OrganizationManagerBuilder {
	b.client = value
	return b
}

// Build creates the manager.
func (b *OrganizationManagerBuilder) Build() (result *OrganizationManager, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.client == nil {
		err = errors.New("IdP client is mandatory")
		return
	}

	result = &OrganizationManager{
		logger: b.logger,
		client: b.client,
	}
	return
}

// OrganizationConfig contains configuration for creating an organization.
type OrganizationConfig struct {
	// Name is the unique identifier for the organization
	Name string

	// DisplayName is the human-readable name
	DisplayName string

	// Enabled indicates whether the organization should be enabled in the identity provider. Nil defaults to true.
	Enabled *bool

	// Domains is the list of e-mail domains associated with the organization.
	Domains []string

	// BreakGlassUsername is the username for the break-glass account
	// If empty, defaults to "osac-break-glass"
	BreakGlassUsername string

	// BreakGlassEmail is the email for the break-glass account
	// If empty, defaults to "break-glass@{organization-name}.osac.local"
	BreakGlassEmail string

	// BreakGlassPassword is the temporary password for the break-glass account
	// This is mandatory and must be changed on first login
	BreakGlassPassword string
}

// BreakGlassCredentials contains the credentials for the break-glass account.
//
// SECURITY NOTES:
//   - Password is plaintext and MUST be handled securely
//   - DO NOT log the password
//   - Store in a secrets manager (Vault, Kubernetes Secrets, AWS Secrets Manager)
//   - Transmit only over TLS
//   - Clear from memory immediately after use
//   - Password is temporary and must be changed on first login
type BreakGlassCredentials struct {
	// UserID is the unique identifier for the break-glass user in the IdP
	UserID string

	// Username is the username for the break-glass account
	Username string

	// Email is the email address for the break-glass account
	Email string

	// Password is the temporary password that must be changed on first login.
	// This field is intentionally excluded from JSON marshaling to prevent
	// accidental logging or exposure.
	Password string `json:"-"`
}

// CreateOrganization creates a complete IdP organization setup with a break-glass account.
// Returns the break-glass account credentials and error.
func (m *OrganizationManager) CreateOrganization(ctx context.Context, config *OrganizationConfig) (*BreakGlassCredentials, error) {
	if config == nil {
		return nil, errors.New("OrganizationConfig is mandatory")
	}

	m.logger.InfoContext(ctx, "Creating IdP organization",
		slog.String("organization", config.Name),
	)

	var (
		// Track if the organization was created in case of error and rollback is needed
		organizationCreated bool
		credentials         *BreakGlassCredentials
		err                 error
	)

	// Defer cleanup on error
	defer func() {
		if err != nil {
			m.logger.ErrorContext(ctx, "Error creating organization",
				slog.String("organization", config.Name),
				slog.Any("error", err),
			)
			m.rollback(ctx, config.Name, organizationCreated)
		}
	}()

	// Step 1: Create the organization
	enabled := true
	if config.Enabled != nil {
		enabled = *config.Enabled
	}
	org := &Organization{
		Name:        config.Name,
		DisplayName: config.DisplayName,
		Enabled:     enabled,
		Domains:     config.Domains,
	}
	createdOrg, err := m.client.CreateOrganization(ctx, org)
	if err != nil {
		return nil, fmt.Errorf("failed to create organization: %w", err)
	}
	organizationCreated = true
	m.logger.InfoContext(ctx, "Organization created",
		slog.String("organization", createdOrg.Name),
	)

	// Step 2: Create break-glass account
	credentials, err = m.createBreakGlassAccount(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create break-glass account: %w", err)
	}

	// Step 3: Assign IdP manager permissions to break-glass account
	err = m.assignIdpManagerPermissions(ctx, credentials.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to assign IdP manager permissions: %w", err)
	}

	m.logger.InfoContext(ctx, "IdP organization created successfully",
		slog.String("organization", createdOrg.Name),
	)
	return credentials, nil
}

// UpdateOrganization updates an existing organization in the identity provider. It fetches the current
// organization by name, applies the updated domains, and sends the update to the IDP.
func (m *OrganizationManager) UpdateOrganization(ctx context.Context, name string, domains []string) error {
	if name == "" {
		return errors.New("organization name is mandatory")
	}

	m.logger.InfoContext(ctx, "Updating IdP organization domains",
		slog.String("organization", name),
	)

	org, err := m.client.GetOrganization(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to get organization for update: %w", err)
	}
	if org == nil {
		return fmt.Errorf("organization '%s' not found", name)
	}

	currentDomains := slices.Clone(org.Domains)
	desiredDomains := slices.Clone(domains)
	slices.Sort(currentDomains)
	slices.Sort(desiredDomains)
	if slices.Equal(currentDomains, desiredDomains) {
		m.logger.DebugContext(ctx, "IdP organization domains already up to date, skipping update",
			slog.String("organization", name),
		)
		return nil
	}

	org.Domains = domains
	_, err = m.client.UpdateOrganization(ctx, org)
	if err != nil {
		return fmt.Errorf("failed to update organization: %w", err)
	}

	m.logger.InfoContext(ctx, "IdP organization domains updated successfully",
		slog.String("organization", name),
	)
	return nil
}

// rollback performs cleanup by deleting the organization.
// Deleting the organization will cascade-delete all resources within it (users, roles, etc.).
func (m *OrganizationManager) rollback(ctx context.Context, organizationName string, deleteOrg bool) {
	if !deleteOrg {
		return
	}

	// Use a fresh context for cleanup so rollback succeeds even if
	// the original context was cancelled or timed out.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m.logger.WarnContext(ctx, "Rolling back organization creation",
		slog.String("organization", organizationName),
	)

	// Delete organization (cascade-deletes all users and resources within it)
	if err := m.client.DeleteOrganization(cleanupCtx, organizationName); err != nil {
		m.logger.ErrorContext(ctx, "Failed to rollback organization deletion",
			slog.String("organization", organizationName),
			slog.Any("error", err),
		)
	} else {
		m.logger.InfoContext(ctx, "Rolled back organization deletion",
			slog.String("organization", organizationName),
		)
	}
}

// createBreakGlassAccount creates the break-glass account for an organization.
// Returns the break-glass credentials and error.
// The break-glass account is a built-in OSAC user with limited privileges (idp-manager role)
// that can manage IdP configuration and roles.
func (m *OrganizationManager) createBreakGlassAccount(ctx context.Context, config *OrganizationConfig) (*BreakGlassCredentials, error) {
	// Set defaults if not provided
	username := config.BreakGlassUsername
	if username == "" {
		username = fmt.Sprintf("%s-osac-break-glass", config.Name)
	}

	email := config.BreakGlassEmail
	if email == "" {
		email = fmt.Sprintf("break-glass@%s.osac.local", config.Name)
	}
	password := config.BreakGlassPassword
	if password == "" {
		// Generate a secure random password using crypto/rand
		const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%"
		const passwordLength = 24
		b := make([]byte, passwordLength)
		for i := range b {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
			if err != nil {
				return nil, fmt.Errorf("failed to generate random password: %w", err)
			}
			b[i] = charset[n.Int64()]
		}
		password = string(b)
		m.logger.DebugContext(ctx, "Generated temporary break-glass password because it was not provided",
			slog.String("organization", config.Name),
			slog.String("username", username),
		)
	}

	user := &User{
		Username:      username,
		Email:         email,
		EmailVerified: true,
		Enabled:       true,
		FirstName:     "OSAC",
		LastName:      "Break-Glass",
		Credentials: []*Credential{
			{
				Type:      "password",
				Value:     password,
				Temporary: true, // User must change password on first login
			},
		},
	}

	createdUser, err := m.client.CreateUser(ctx, config.Name, user)
	if err != nil {
		return nil, err
	}

	credentials := &BreakGlassCredentials{
		UserID:   createdUser.ID,
		Username: username,
		Email:    email,
		Password: password,
	}

	m.logger.InfoContext(ctx, "Break-glass account created for organization",
		slog.String("organization_name", config.Name),
		slog.String("username", username),
		slog.String("user_id", createdUser.ID),
	)

	return credentials, nil
}

// assignIdpManagerPermissions assigns limited IdP manager permissions to a user.
// This grants the user permissions to manage user roles and identity providers but not
// critical realm settings.
// The implementation is provider-specific (delegated to the IdP client).
func (m *OrganizationManager) assignIdpManagerPermissions(ctx context.Context, userID string) error {
	m.logger.InfoContext(ctx, "Assigning IdP manager permissions to user",
		slog.String("user_id", userID),
	)

	err := m.client.AssignIdpManagerPermissions(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to assign IdP manager permissions: %w", err)
	}

	m.logger.InfoContext(ctx, "IdP manager permissions assigned",
		slog.String("user_id", userID),
	)
	return nil
}

// DeleteOrganization deletes an IdP organization and all its resources.
// The implementation handles provider-specific cleanup (e.g., Keycloak deletes break-glass account first).
func (m *OrganizationManager) DeleteOrganization(ctx context.Context, organizationName string) error {
	m.logger.InfoContext(ctx, "Deleting IdP organization",
		slog.String("organization", organizationName),
	)

	err := m.client.DeleteOrganization(ctx, organizationName)
	if err != nil {
		return fmt.Errorf("failed to delete organization: %w", err)
	}

	m.logger.InfoContext(ctx, "IdP organization deleted successfully",
		slog.String("organization", organizationName),
	)
	return nil
}
