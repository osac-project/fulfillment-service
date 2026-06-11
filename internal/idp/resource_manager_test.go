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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("ResourceManager", func() {
	var (
		ctx           context.Context
		ctrl          *gomock.Controller
		mockClient    *MockClient
		manager       *ResourceManager
		testProjectID string
		testTenant    string
		testProject   string
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockClient = NewMockClient(ctrl)

		testProjectID = "project-123"
		testTenant = "acme"
		testProject = "web-app"

		var err error
		manager, err = NewResourceManager().
			SetLogger(logger).
			SetClient(mockClient).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("Builder validation", func() {
		It("should fail when logger is not set", func() {
			_, err := NewResourceManager().
				SetClient(mockClient).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger is mandatory"))
		})

		It("should fail when client is not set", func() {
			_, err := NewResourceManager().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("IdP client is mandatory"))
		})
	})

	Describe("CreateProjectAuthorizationResource", func() {
		It("should create an authorization resource with correct naming format and groups", func() {
			expectedResourceName := "PROJECT-acme-web-app"
			expectedResourceID := "resource-456"
			testScopes := []string{ScopeViewProject, ScopeManageProject}

			// Expect resource creation
			mockClient.EXPECT().
				CreateAuthorizationResource(ctx, gomock.Any()).
				DoAndReturn(func(ctx context.Context, resource *AuthorizationResource) (*AuthorizationResource, error) {
					// Verify resource structure
					Expect(resource.Name).To(Equal(expectedResourceName))
					Expect(resource.Scopes).To(ConsistOf(ScopeViewProject, ScopeManageProject))
					Expect(resource.Attributes["project_id"]).To(Equal([]string{testProjectID}))
					Expect(resource.Attributes["tenant"]).To(Equal([]string{testTenant}))

					// Return created resource with ID
					return &AuthorizationResource{
						ID:         expectedResourceID,
						Name:       resource.Name,
						Scopes:     resource.Scopes,
						Attributes: resource.Attributes,
					}, nil
				})

			// Expect viewers group creation (new organization groups API)
			mockClient.EXPECT().
				CreateAuthorizationGroup(ctx, "acme", "viewers", "/web-app/viewers").
				Return(nil)

			// Expect managers group creation (new organization groups API)
			mockClient.EXPECT().
				CreateAuthorizationGroup(ctx, "acme", "managers", "/web-app/managers").
				Return(nil)

			// Expect viewers policy creation
			mockClient.EXPECT().
				CreateGroupPolicy(ctx, gomock.Any()).
				DoAndReturn(func(ctx context.Context, policy *AuthorizationPolicy) (*AuthorizationPolicy, error) {
					Expect(policy.Name).To(Equal("web-app-viewers-policy"))
					Expect(policy.Type).To(Equal("group"))
					Expect(policy.Logic).To(Equal("POSITIVE"))
					return &AuthorizationPolicy{
						ID:    "viewers-policy-id",
						Name:  policy.Name,
						Type:  policy.Type,
						Logic: policy.Logic,
					}, nil
				})

			// Expect managers policy creation
			mockClient.EXPECT().
				CreateGroupPolicy(ctx, gomock.Any()).
				DoAndReturn(func(ctx context.Context, policy *AuthorizationPolicy) (*AuthorizationPolicy, error) {
					Expect(policy.Name).To(Equal("web-app-managers-policy"))
					Expect(policy.Type).To(Equal("group"))
					Expect(policy.Logic).To(Equal("POSITIVE"))
					return &AuthorizationPolicy{
						ID:    "managers-policy-id",
						Name:  policy.Name,
						Type:  policy.Type,
						Logic: policy.Logic,
					}, nil
				})

			// Expect viewers permission creation (VIEW_PROJECT scope)
			mockClient.EXPECT().
				CreateScopePermission(ctx, gomock.Any()).
				DoAndReturn(func(ctx context.Context, permission *AuthorizationPermission) (*AuthorizationPermission, error) {
					Expect(permission.Name).To(Equal("web-app-view-permission"))
					Expect(permission.Type).To(Equal("scope"))
					Expect(permission.Scopes).To(ConsistOf(ScopeViewProject))
					return &AuthorizationPermission{
						ID:         "viewers-permission-id",
						Name:       permission.Name,
						Type:       permission.Type,
						ResourceID: permission.ResourceID,
						Scopes:     permission.Scopes,
					}, nil
				})

			// Expect managers permission creation (MANAGE_PROJECT scope)
			mockClient.EXPECT().
				CreateScopePermission(ctx, gomock.Any()).
				DoAndReturn(func(ctx context.Context, permission *AuthorizationPermission) (*AuthorizationPermission, error) {
					Expect(permission.Name).To(Equal("web-app-manage-permission"))
					Expect(permission.Type).To(Equal("scope"))
					Expect(permission.Scopes).To(ConsistOf(ScopeManageProject))
					return &AuthorizationPermission{
						ID:         "managers-permission-id",
						Name:       permission.Name,
						Type:       permission.Type,
						ResourceID: permission.ResourceID,
						Scopes:     permission.Scopes,
					}, nil
				})

			resourceID, err := manager.CreateProjectAuthorizationResource(ctx, testProjectID, testTenant, testProject, testScopes)

			Expect(err).ToNot(HaveOccurred())
			Expect(resourceID).To(Equal(expectedResourceID))
		})

		It("should return error when resource creation fails", func() {
			testScopes := []string{ScopeViewProject, ScopeManageProject}

			mockClient.EXPECT().
				CreateAuthorizationResource(ctx, gomock.Any()).
				Return(nil, context.DeadlineExceeded)

			_, err := manager.CreateProjectAuthorizationResource(ctx, testProjectID, testTenant, testProject, testScopes)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create authorization resource"))
		})

		It("should cleanup resource when viewers group creation fails", func() {
			expectedResourceID := "resource-456"
			testScopes := []string{ScopeViewProject, ScopeManageProject}

			// Resource creation succeeds
			mockClient.EXPECT().
				CreateAuthorizationResource(ctx, gomock.Any()).
				Return(&AuthorizationResource{
					ID:   expectedResourceID,
					Name: "PROJECT-acme-web-app",
				}, nil)

			// Viewers group creation fails
			mockClient.EXPECT().
				CreateAuthorizationGroup(ctx, "acme", "viewers", "/web-app/viewers").
				Return(context.DeadlineExceeded)

			// Expect cleanup: delete the resource
			mockClient.EXPECT().
				DeleteAuthorizationResource(ctx, expectedResourceID).
				Return(nil)

			_, err := manager.CreateProjectAuthorizationResource(ctx, testProjectID, testTenant, testProject, testScopes)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create authorization groups"))
		})

		It("should cleanup resource and viewers group when managers group creation fails", func() {
			expectedResourceID := "resource-456"
			testScopes := []string{ScopeViewProject, ScopeManageProject}

			// Resource creation succeeds
			mockClient.EXPECT().
				CreateAuthorizationResource(ctx, gomock.Any()).
				Return(&AuthorizationResource{
					ID:   expectedResourceID,
					Name: "PROJECT-acme-web-app",
				}, nil)

			// Viewers group creation succeeds
			mockClient.EXPECT().
				CreateAuthorizationGroup(ctx, "acme", "viewers", "/web-app/viewers").
				Return(nil)

			// Managers group creation fails
			mockClient.EXPECT().
				CreateAuthorizationGroup(ctx, "acme", "managers", "/web-app/managers").
				Return(context.DeadlineExceeded)

			// Expect cleanup: get viewers group ID, then delete it
			mockClient.EXPECT().
				GetGroupIDByPath(ctx, "acme", "/web-app/viewers").
				Return("viewers-group-id", nil)
			mockClient.EXPECT().
				DeleteAuthorizationGroup(ctx, "acme", "viewers-group-id").
				Return(nil)

			// Expect cleanup: delete the resource
			mockClient.EXPECT().
				DeleteAuthorizationResource(ctx, expectedResourceID).
				Return(nil)

			_, err := manager.CreateProjectAuthorizationResource(ctx, testProjectID, testTenant, testProject, testScopes)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create authorization groups"))
		})
	})

	Describe("DeleteAuthorizationResource", func() {
		It("should delete an authorization resource and its groups by ID", func() {
			resourceID := "resource-456"
			resourceName := "PROJECT-acme-web-app"

			// Get resource to extract name and tenant for group cleanup
			mockClient.EXPECT().
				GetAuthorizationResource(ctx, resourceID).
				Return(&AuthorizationResource{
					ID:   resourceID,
					Name: resourceName,
					Attributes: map[string][]string{
						"tenant": {"acme"},
					},
				}, nil)

			// Get viewers group ID (new organization groups API)
			mockClient.EXPECT().
				GetGroupIDByPath(ctx, "acme", "/web-app/viewers").
				Return("viewers-group-id", nil)

			// Delete viewers group (new organization groups API)
			mockClient.EXPECT().
				DeleteAuthorizationGroup(ctx, "acme", "viewers-group-id").
				Return(nil)

			// Get managers group ID (new organization groups API)
			mockClient.EXPECT().
				GetGroupIDByPath(ctx, "acme", "/web-app/managers").
				Return("managers-group-id", nil)

			// Delete managers group (new organization groups API)
			mockClient.EXPECT().
				DeleteAuthorizationGroup(ctx, "acme", "managers-group-id").
				Return(nil)

			// Delete the resource itself
			mockClient.EXPECT().
				DeleteAuthorizationResource(ctx, resourceID).
				Return(nil)

			err := manager.DeleteAuthorizationResource(ctx, resourceID)

			Expect(err).ToNot(HaveOccurred())
		})

		It("should continue deleting resource even if GetAuthorizationResource fails", func() {
			resourceID := "resource-456"

			// Get resource fails (might already be deleted)
			mockClient.EXPECT().
				GetAuthorizationResource(ctx, resourceID).
				Return(nil, context.DeadlineExceeded)

			// Should still attempt to delete the resource
			mockClient.EXPECT().
				DeleteAuthorizationResource(ctx, resourceID).
				Return(nil)

			err := manager.DeleteAuthorizationResource(ctx, resourceID)

			Expect(err).ToNot(HaveOccurred())
		})

		It("should continue deleting resource even if group deletion fails", func() {
			resourceID := "resource-456"
			resourceName := "PROJECT-acme-web-app"

			// Get resource succeeds with tenant attribute
			mockClient.EXPECT().
				GetAuthorizationResource(ctx, resourceID).
				Return(&AuthorizationResource{
					ID:   resourceID,
					Name: resourceName,
					Attributes: map[string][]string{
						"tenant": {"acme"},
					},
				}, nil)

			// Viewers group lookup fails
			mockClient.EXPECT().
				GetGroupIDByPath(ctx, "acme", "/web-app/viewers").
				Return("", context.DeadlineExceeded)

			// Managers group lookup succeeds
			mockClient.EXPECT().
				GetGroupIDByPath(ctx, "acme", "/web-app/managers").
				Return("managers-group-id", nil)

			// Managers group deletion fails
			mockClient.EXPECT().
				DeleteAuthorizationGroup(ctx, "acme", "managers-group-id").
				Return(context.DeadlineExceeded)

			// Should still delete the resource
			mockClient.EXPECT().
				DeleteAuthorizationResource(ctx, resourceID).
				Return(nil)

			err := manager.DeleteAuthorizationResource(ctx, resourceID)

			Expect(err).ToNot(HaveOccurred())
		})

		It("should return error when resource deletion fails", func() {
			resourceID := "resource-456"

			// Get resource fails
			mockClient.EXPECT().
				GetAuthorizationResource(ctx, resourceID).
				Return(nil, context.DeadlineExceeded)

			// Resource deletion fails
			mockClient.EXPECT().
				DeleteAuthorizationResource(ctx, resourceID).
				Return(context.DeadlineExceeded)

			err := manager.DeleteAuthorizationResource(ctx, resourceID)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to delete authorization resource"))
		})
	})

	Describe("GetAuthorizationResource", func() {
		It("should retrieve an authorization resource by ID", func() {
			resourceID := "resource-456"
			expectedResource := &AuthorizationResource{
				ID:   resourceID,
				Name: "PROJECT-acme-web-app",
				Scopes: []string{
					ScopeViewProject,
					ScopeManageProject,
				},
			}

			mockClient.EXPECT().
				GetAuthorizationResource(ctx, resourceID).
				Return(expectedResource, nil)

			resource, err := manager.GetAuthorizationResource(ctx, resourceID)

			Expect(err).ToNot(HaveOccurred())
			Expect(resource).To(Equal(expectedResource))
		})

		It("should return error when client fails", func() {
			resourceID := "resource-456"

			mockClient.EXPECT().
				GetAuthorizationResource(ctx, resourceID).
				Return(nil, context.DeadlineExceeded)

			_, err := manager.GetAuthorizationResource(ctx, resourceID)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get authorization resource"))
		})
	})
})
