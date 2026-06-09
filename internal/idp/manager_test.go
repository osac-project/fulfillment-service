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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// mockClient is a mock IdP client for testing.
type mockClient struct {
	createdRealm        *Organization
	createdUsers        []*User
	deletedRealm        string
	deletedUsers        []string                      // Track deleted user IDs
	userRoleAssignments map[string]map[string][]*Role // userID -> clientID -> roles
	failUserCreation    bool                          // Trigger user creation failure
	failRoleAssignment  bool                          // Trigger role assignment failure
	failOrgDeletion     bool                          // Trigger organization deletion failure
	failOrgGet          bool                          // Trigger organization get failure
	failOrgUpdate       bool                          // Trigger organization update failure
	returnNilOrg        bool                          // GetOrganization returns nil without error
	orgUpdateCalled     bool                          // Track whether UpdateOrganization was called
}

func (m *mockClient) CreateOrganization(ctx context.Context, org *Organization) (*Organization, error) {
	// Create a copy to avoid mutation
	createdOrg := &Organization{
		ID:          org.ID,
		Name:        org.Name,
		DisplayName: org.DisplayName,
		Enabled:     org.Enabled,
		Attributes:  org.Attributes,
		Domains:     org.Domains,
	}
	m.createdRealm = createdOrg
	return createdOrg, nil
}

func (m *mockClient) GetOrganization(ctx context.Context, name string) (*Organization, error) {
	if m.failOrgGet {
		return nil, fmt.Errorf("simulated get organization failure")
	}
	if m.returnNilOrg {
		return nil, nil
	}
	return m.createdRealm, nil
}

func (m *mockClient) UpdateOrganization(ctx context.Context, org *Organization) (*Organization, error) {
	m.orgUpdateCalled = true
	if m.failOrgUpdate {
		return nil, fmt.Errorf("simulated update organization failure")
	}
	if m.createdRealm != nil {
		m.createdRealm.Domains = org.Domains
		m.createdRealm.Enabled = org.Enabled
	}
	return m.createdRealm, nil
}

func (m *mockClient) DeleteOrganization(ctx context.Context, name string) error {
	if m.failOrgDeletion {
		return fmt.Errorf("simulated organization deletion failure")
	}

	// Simulate Keycloak behavior: delete break-glass account first
	breakGlassUsername := fmt.Sprintf("%s-osac-break-glass", name)
	for _, user := range m.createdUsers {
		if user.Username == breakGlassUsername {
			m.deletedUsers = append(m.deletedUsers, user.ID)
			break
		}
	}

	m.deletedRealm = name
	return nil
}

func (m *mockClient) CreateUser(ctx context.Context, organization string, user *User) (*User, error) {
	if m.failUserCreation {
		return nil, fmt.Errorf("simulated user creation failure")
	}
	// Create a copy with ID populated
	userID := fmt.Sprintf("user-%d", len(m.createdUsers)+1)
	createdUser := &User{
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
	m.createdUsers = append(m.createdUsers, createdUser)
	return createdUser, nil
}

func (m *mockClient) GetUser(ctx context.Context, organization, userID string) (*User, error) {
	for _, user := range m.createdUsers {
		if user.ID == userID {
			return user, nil
		}
	}
	return nil, nil
}

func (m *mockClient) ListUsers(ctx context.Context, organization string) ([]*User, error) {
	return m.createdUsers, nil
}

func (m *mockClient) DeleteUser(ctx context.Context, organization, userID string) error {
	m.deletedUsers = append(m.deletedUsers, userID)
	return nil
}

func (m *mockClient) ListOrganizationRoles(ctx context.Context, organization string) ([]*Role, error) {
	return nil, nil
}

func (m *mockClient) ListClientRoles(ctx context.Context, organization, clientID string) ([]*Role, error) {
	// Return full set of realm-management roles (matching Keycloak's standard roles)
	if clientID == "realm-management" {
		return []*Role{
			{ID: "1", Name: "manage-realm", ClientRole: true},
			{ID: "2", Name: "manage-users", ClientRole: true},
			{ID: "3", Name: "manage-clients", ClientRole: true},
			{ID: "4", Name: "manage-identity-providers", ClientRole: true},
			{ID: "5", Name: "manage-authorization", ClientRole: true},
			{ID: "6", Name: "manage-events", ClientRole: true},
			{ID: "7", Name: "view-realm", ClientRole: true},
			{ID: "8", Name: "view-users", ClientRole: true},
			{ID: "9", Name: "view-clients", ClientRole: true},
			{ID: "10", Name: "view-identity-providers", ClientRole: true},
			{ID: "11", Name: "view-authorization", ClientRole: true},
			{ID: "12", Name: "view-events", ClientRole: true},
		}, nil
	}
	return nil, nil
}

func (m *mockClient) AssignOrganizationRolesToUser(ctx context.Context, organization, userID string, roles []*Role) error {
	if m.userRoleAssignments == nil {
		m.userRoleAssignments = make(map[string]map[string][]*Role)
	}
	if m.userRoleAssignments[userID] == nil {
		m.userRoleAssignments[userID] = make(map[string][]*Role)
	}
	m.userRoleAssignments[userID]["realm"] = roles
	return nil
}

func (m *mockClient) AssignClientRolesToUser(ctx context.Context, organization, userID, clientID string, roles []*Role) error {
	if m.userRoleAssignments == nil {
		m.userRoleAssignments = make(map[string]map[string][]*Role)
	}
	if m.userRoleAssignments[userID] == nil {
		m.userRoleAssignments[userID] = make(map[string][]*Role)
	}
	m.userRoleAssignments[userID][clientID] = roles
	return nil
}

func (m *mockClient) RemoveOrganizationRolesFromUser(ctx context.Context, organization, userID string, roles []*Role) error {
	return nil
}

func (m *mockClient) RemoveClientRolesFromUser(ctx context.Context, organization, userID, clientID string, roles []*Role) error {
	return nil
}

func (m *mockClient) GetUserOrganizationRoles(ctx context.Context, organization, userID string) ([]*Role, error) {
	if m.userRoleAssignments != nil && m.userRoleAssignments[userID] != nil {
		return m.userRoleAssignments[userID]["realm"], nil
	}
	return nil, nil
}

func (m *mockClient) GetUserClientRoles(ctx context.Context, organization, userID, clientID string) ([]*Role, error) {
	if m.userRoleAssignments != nil && m.userRoleAssignments[userID] != nil {
		return m.userRoleAssignments[userID][clientID], nil
	}
	return nil, nil
}

func (m *mockClient) AssignOrganizationAdminPermissions(ctx context.Context, organization, userID string) error {
	if m.failRoleAssignment {
		return fmt.Errorf("simulated role assignment failure")
	}
	// Simulate assigning full admin roles (matching keycloakRealmManagementRoles)
	roles := []*Role{
		{ID: "1", Name: "manage-realm", ClientRole: true},
		{ID: "2", Name: "manage-users", ClientRole: true},
		{ID: "3", Name: "manage-clients", ClientRole: true},
		{ID: "4", Name: "manage-identity-providers", ClientRole: true},
		{ID: "5", Name: "manage-authorization", ClientRole: true},
		{ID: "6", Name: "manage-events", ClientRole: true},
		{ID: "7", Name: "view-realm", ClientRole: true},
		{ID: "8", Name: "view-users", ClientRole: true},
		{ID: "9", Name: "view-clients", ClientRole: true},
		{ID: "10", Name: "view-identity-providers", ClientRole: true},
		{ID: "11", Name: "view-authorization", ClientRole: true},
		{ID: "12", Name: "view-events", ClientRole: true},
	}
	return m.AssignClientRolesToUser(ctx, organization, userID, "realm-management", roles)
}

func (m *mockClient) AssignIdpManagerPermissions(ctx context.Context, userID string) error {
	if m.failRoleAssignment {
		return fmt.Errorf("simulated role assignment failure")
	}
	// Simulate assigning limited IdP manager roles (matching keycloakIdpManagerRoles)
	roles := []*Role{
		{ID: "2", Name: "manage-users", ClientRole: true},
		{ID: "8", Name: "view-users", ClientRole: true},
		{ID: "4", Name: "manage-identity-providers", ClientRole: true},
		{ID: "10", Name: "view-identity-providers", ClientRole: true},
		{ID: "7", Name: "view-realm", ClientRole: true},
	}
	// Use empty organization name since it's no longer a parameter
	return m.AssignClientRolesToUser(ctx, "", userID, "realm-management", roles)
}

func (m *mockClient) CreateAuthorizationResource(ctx context.Context, resource *AuthorizationResource) (*AuthorizationResource, error) {
	return &AuthorizationResource{
		ID:         "resource-id",
		Name:       resource.Name,
		Type:       resource.Type,
		Scopes:     resource.Scopes,
		Attributes: resource.Attributes,
	}, nil
}

func (m *mockClient) GetAuthorizationResource(ctx context.Context, resourceID string) (*AuthorizationResource, error) {
	return &AuthorizationResource{
		ID:   resourceID,
		Name: "PROJECT-test-project",
	}, nil
}

func (m *mockClient) DeleteAuthorizationResource(ctx context.Context, resourceID string) error {
	return nil
}

// Identity Provider stub methods
func (m *mockClient) GetIdentityProvider(ctx context.Context, alias string) (*IdentityProvider, error) {
	return nil, nil
}

func (m *mockClient) ListAllIdentityProviders(ctx context.Context) ([]*IdentityProvider, error) {
	return nil, nil
}

func (m *mockClient) GetOrganizationIdentityProvider(ctx context.Context, organizationName, alias string) (*IdentityProvider, error) {
	return nil, nil
}

func (m *mockClient) ListIdentityProviders(ctx context.Context, organizationName string) ([]*IdentityProvider, error) {
	return nil, nil
}

func (m *mockClient) CreateAuthorizationGroup(ctx context.Context, organizationName, groupName, groupPath string) error {
	return nil
}

func (m *mockClient) DeleteAuthorizationGroup(ctx context.Context, organizationName, groupID string) error {
	return nil
}

func (m *mockClient) GetGroupIDByPath(ctx context.Context, organizationName, groupPath string) (string, error) {
	// Return a fake group ID for testing
	return "test-group-id", nil
}

var _ = Describe("OrganizationManager", func() {
	var (
		ctx     context.Context
		mock    *mockClient
		manager *OrganizationManager
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()
		mock = &mockClient{}

		manager, err = NewOrganizationManager().
			SetLogger(logger).
			SetClient(mock).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("CreateOrganization", func() {
		It("creates an organization with break-glass account", func() {
			config := &OrganizationConfig{
				Name:               "test-org",
				DisplayName:        "Test Organization",
				Enabled:            new(true),
				BreakGlassPassword: "breakglass123",
			}

			credentials, err := manager.CreateOrganization(ctx, config)
			Expect(err).ToNot(HaveOccurred())
			Expect(credentials).ToNot(BeNil())

			// Verify realm was created
			Expect(mock.createdRealm).ToNot(BeNil())
			Expect(mock.createdRealm.Name).To(Equal("test-org"))
			Expect(mock.createdRealm.Enabled).To(BeTrue())

			// Verify break-glass user was created
			Expect(mock.createdUsers).To(HaveLen(1))
			breakGlassUser := mock.createdUsers[0]
			Expect(breakGlassUser.Username).To(Equal("test-org-osac-break-glass"))
			Expect(breakGlassUser.Email).To(Equal("break-glass@test-org.osac.local"))
			Expect(breakGlassUser.FirstName).To(Equal("OSAC"))
			Expect(breakGlassUser.LastName).To(Equal("Break-Glass"))

			// Verify credentials were returned
			Expect(credentials.UserID).To(Equal(breakGlassUser.ID))
			Expect(credentials.Username).To(Equal("test-org-osac-break-glass"))
			Expect(credentials.Email).To(Equal("break-glass@test-org.osac.local"))
			Expect(credentials.Password).To(Equal("breakglass123"))

			// Verify password is temporary
			Expect(breakGlassUser.Credentials).To(HaveLen(1))
			Expect(breakGlassUser.Credentials[0].Temporary).To(BeTrue())
		})

		It("creates a disabled organization when Enabled is false", func() {
			config := &OrganizationConfig{
				Name:               "test-org",
				DisplayName:        "Test Organization",
				Enabled:            new(false),
				BreakGlassPassword: "breakglass123",
			}

			credentials, err := manager.CreateOrganization(ctx, config)
			Expect(err).ToNot(HaveOccurred())
			Expect(credentials).ToNot(BeNil())

			Expect(mock.createdRealm).ToNot(BeNil())
			Expect(mock.createdRealm.Name).To(Equal("test-org"))
			Expect(mock.createdRealm.Enabled).To(BeFalse())
		})

		It("assigns IdP manager roles to break-glass account", func() {
			config := &OrganizationConfig{
				Name:               "test-org",
				DisplayName:        "Test Organization",
				Enabled:            new(true),
				BreakGlassPassword: "breakglass123",
			}

			credentials, err := manager.CreateOrganization(ctx, config)
			Expect(err).ToNot(HaveOccurred())
			Expect(credentials).ToNot(BeNil())
			Expect(credentials.Username).ToNot(BeEmpty())
			Expect(credentials.Email).ToNot(BeEmpty())
			Expect(credentials.Password).ToNot(BeEmpty())
			Expect(credentials.UserID).ToNot(BeEmpty())

			// Verify break-glass user was created
			Expect(mock.createdUsers).To(HaveLen(1))
			breakGlassUserID := mock.createdUsers[0].ID
			Expect(credentials.UserID).To(Equal(breakGlassUserID))

			// Verify IdP manager roles were assigned
			Expect(mock.userRoleAssignments).ToNot(BeNil())
			Expect(mock.userRoleAssignments[breakGlassUserID]).ToNot(BeNil())

			// Check for realm-management client role assignments (limited set)
			breakGlassRoles := mock.userRoleAssignments[breakGlassUserID]["realm-management"]
			Expect(breakGlassRoles).ToNot(BeEmpty())

			// Verify specific IdP manager roles were assigned
			roleNames := make([]string, len(breakGlassRoles))
			for i, role := range breakGlassRoles {
				roleNames[i] = role.Name
			}

			// Should contain all 5 IdP manager roles
			Expect(roleNames).To(ContainElement("manage-users"))
			Expect(roleNames).To(ContainElement("view-users"))
			Expect(roleNames).To(ContainElement("manage-identity-providers"))
			Expect(roleNames).To(ContainElement("view-identity-providers"))
			Expect(roleNames).To(ContainElement("view-realm"))
			// Should NOT contain full admin roles like manage-realm or manage-clients
			Expect(roleNames).ToNot(ContainElement("manage-realm"))
			Expect(roleNames).ToNot(ContainElement("manage-clients"))
		})

		It("uses custom break-glass username and email when provided", func() {
			config := &OrganizationConfig{
				Name:               "test-org",
				DisplayName:        "Test Organization",
				Enabled:            new(true),
				BreakGlassUsername: "custom-break-glass",
				BreakGlassEmail:    "custom@example.com",
				BreakGlassPassword: "breakglass123",
			}

			credentials, err := manager.CreateOrganization(ctx, config)
			Expect(err).ToNot(HaveOccurred())

			Expect(mock.createdUsers).To(HaveLen(1))
			breakGlassUser := mock.createdUsers[0]
			Expect(breakGlassUser.Username).To(Equal("custom-break-glass"))
			Expect(breakGlassUser.Email).To(Equal("custom@example.com"))

			Expect(credentials.Username).To(Equal("custom-break-glass"))
			Expect(credentials.Email).To(Equal("custom@example.com"))
			Expect(credentials.Password).To(Equal("breakglass123"))
			Expect(credentials.UserID).To(Equal(breakGlassUser.ID))
		})

		It("generates password when not provided", func() {
			config := &OrganizationConfig{
				Name:        "test-org",
				DisplayName: "Test Organization",
				Enabled:     new(true),
			}

			credentials, err := manager.CreateOrganization(ctx, config)
			Expect(err).ToNot(HaveOccurred())
			Expect(credentials.Username).To(Equal("test-org-osac-break-glass"))
			Expect(credentials.Email).To(Equal("break-glass@test-org.osac.local"))
			Expect(credentials.Password).ToNot(BeEmpty())
			Expect(credentials.Password).To(HaveLen(24))
			// Password should contain characters from the defined charset
			Expect(credentials.Password).To(MatchRegexp(`^[A-Za-z0-9!@#$%]{24}$`))
		})

		It("rolls back organization on break-glass user creation failure", func() {
			// Create a mock that fails on user creation
			failingMock := &mockClient{
				failUserCreation: true,
			}

			failingManager, err := NewOrganizationManager().
				SetLogger(logger).
				SetClient(failingMock).
				Build()
			Expect(err).ToNot(HaveOccurred())

			config := &OrganizationConfig{
				Name:               "test-org",
				DisplayName:        "Test Organization",
				Enabled:            new(true),
				BreakGlassPassword: "breakglass123",
			}

			credentials, err := failingManager.CreateOrganization(ctx, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create break-glass account"))
			Expect(credentials).To(BeNil())

			// Verify organization was created then deleted (rollback)
			Expect(failingMock.createdRealm).ToNot(BeNil())
			Expect(failingMock.deletedRealm).To(Equal("test-org"))
		})

		It("rolls back organization on role assignment failure", func() {
			// Create a mock that fails on role assignment
			failingMock := &mockClient{
				failRoleAssignment: true,
			}

			failingManager, err := NewOrganizationManager().
				SetLogger(logger).
				SetClient(failingMock).
				Build()
			Expect(err).ToNot(HaveOccurred())

			config := &OrganizationConfig{
				Name:               "test-org",
				DisplayName:        "Test Organization",
				Enabled:            new(true),
				BreakGlassPassword: "breakglass123",
			}

			credentials, err := failingManager.CreateOrganization(ctx, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to assign IdP manager permissions"))
			Expect(credentials).To(BeNil())

			// Verify user was created
			Expect(failingMock.createdUsers).To(HaveLen(1))

			// Verify organization was created then deleted (rollback)
			// Deleting the organization cascade-deletes all users, so we don't
			// need to explicitly delete the user
			Expect(failingMock.createdRealm).ToNot(BeNil())
			Expect(failingMock.deletedRealm).To(Equal("test-org"))
		})

		It("rolls back organization even when original context is cancelled", func() {
			// Create a mock that fails on user creation
			failingMock := &mockClient{
				failUserCreation: true,
			}

			failingManager, err := NewOrganizationManager().
				SetLogger(logger).
				SetClient(failingMock).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create a context that is already cancelled
			cancelledCtx, cancel := context.WithCancel(context.Background())
			cancel()

			config := &OrganizationConfig{
				Name:               "test-org",
				DisplayName:        "Test Organization",
				Enabled:            new(true),
				BreakGlassPassword: "breakglass123",
			}

			credentials, err := failingManager.CreateOrganization(cancelledCtx, config)
			Expect(err).To(HaveOccurred())
			Expect(credentials).To(BeNil())

			// Verify organization was created then deleted (rollback)
			// Even though the original context was cancelled, rollback should succeed
			// because it uses a fresh context
			Expect(failingMock.createdRealm).ToNot(BeNil())
			Expect(failingMock.deletedRealm).To(Equal("test-org"))
		})
	})

	Describe("UpdateOrganization", func() {
		It("skips the IDP update when domains already match", func() {
			config := &OrganizationConfig{
				Name:               "test-org",
				Enabled:            new(true),
				Domains:            []string{"a.example.com", "b.example.com"},
				BreakGlassPassword: "breakglass123",
			}
			_, err := manager.CreateOrganization(ctx, config)
			Expect(err).ToNot(HaveOccurred())

			mock.orgUpdateCalled = false
			err = manager.UpdateOrganization(
				ctx, "test-org", []string{"b.example.com", "a.example.com"},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(mock.orgUpdateCalled).To(BeFalse())
			Expect(mock.createdRealm.Domains).To(ConsistOf("a.example.com", "b.example.com"))
		})

		It("updates the organization domains", func() {
			config := &OrganizationConfig{
				Name:               "test-org",
				Enabled:            new(true),
				Domains:            []string{"example.com"},
				BreakGlassPassword: "breakglass123",
			}
			_, err := manager.CreateOrganization(ctx, config)
			Expect(err).ToNot(HaveOccurred())

			err = manager.UpdateOrganization(ctx, "test-org", []string{"new.example.com", "corp.example.org"})
			Expect(err).ToNot(HaveOccurred())
			Expect(mock.createdRealm.Domains).To(ConsistOf("new.example.com", "corp.example.org"))
		})

		It("clears domains when given an empty list", func() {
			config := &OrganizationConfig{
				Name:               "test-org",
				Enabled:            new(true),
				Domains:            []string{"example.com"},
				BreakGlassPassword: "breakglass123",
			}
			_, err := manager.CreateOrganization(ctx, config)
			Expect(err).ToNot(HaveOccurred())
			Expect(mock.createdRealm.Domains).To(ConsistOf("example.com"))

			err = manager.UpdateOrganization(ctx, "test-org", []string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(mock.createdRealm.Domains).To(BeEmpty())
		})

		It("clears domains when given nil", func() {
			config := &OrganizationConfig{
				Name:               "test-org",
				Enabled:            new(true),
				Domains:            []string{"example.com"},
				BreakGlassPassword: "breakglass123",
			}
			_, err := manager.CreateOrganization(ctx, config)
			Expect(err).ToNot(HaveOccurred())

			err = manager.UpdateOrganization(ctx, "test-org", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(mock.createdRealm.Domains).To(BeEmpty())
		})

		It("returns an error when the name is empty", func() {
			err := manager.UpdateOrganization(ctx, "", []string{"example.com"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("organization name is mandatory"))
		})

		It("returns an error when getting the organization fails", func() {
			failingMock := &mockClient{
				failOrgGet: true,
			}
			failingManager, err := NewOrganizationManager().
				SetLogger(logger).
				SetClient(failingMock).
				Build()
			Expect(err).ToNot(HaveOccurred())

			err = failingManager.UpdateOrganization(ctx, "test-org", []string{"example.com"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get organization for update"))
		})

		It("returns an error when the organization does not exist", func() {
			failingMock := &mockClient{
				returnNilOrg: true,
			}
			failingManager, err := NewOrganizationManager().
				SetLogger(logger).
				SetClient(failingMock).
				Build()
			Expect(err).ToNot(HaveOccurred())

			err = failingManager.UpdateOrganization(ctx, "missing-org", []string{"example.com"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("returns an error when the client update fails", func() {
			failingMock := &mockClient{
				createdRealm:  &Organization{Name: "test-org"},
				failOrgUpdate: true,
			}
			failingManager, err := NewOrganizationManager().
				SetLogger(logger).
				SetClient(failingMock).
				Build()
			Expect(err).ToNot(HaveOccurred())

			err = failingManager.UpdateOrganization(ctx, "test-org", []string{"example.com"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to update organization"))
		})
	})

	Describe("DeleteOrganization", func() {
		It("deletes the organization realm", func() {
			err := manager.DeleteOrganization(ctx, "test-org")
			Expect(err).ToNot(HaveOccurred())
			Expect(mock.deletedRealm).To(Equal("test-org"))
		})

		It("returns an error when deletion fails", func() {
			failingMock := &mockClient{
				failOrgDeletion: true,
			}

			failingManager, err := NewOrganizationManager().
				SetLogger(logger).
				SetClient(failingMock).
				Build()
			Expect(err).ToNot(HaveOccurred())

			err = failingManager.DeleteOrganization(ctx, "test-org")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to delete organization"))
		})
	})
})
