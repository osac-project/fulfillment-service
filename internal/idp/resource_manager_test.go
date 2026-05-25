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
		It("should create an authorization resource with correct naming format", func() {
			expectedResourceName := "PROJECT-acme-web-app"
			expectedResourceID := "resource-456"
			testScopes := []string{ScopeViewProject, ScopeManageProject}

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

			resourceID, err := manager.CreateProjectAuthorizationResource(ctx, testProjectID, testTenant, testProject, testScopes)

			Expect(err).ToNot(HaveOccurred())
			Expect(resourceID).To(Equal(expectedResourceID))
		})

		It("should return error when client fails", func() {
			testScopes := []string{ScopeViewProject, ScopeManageProject}

			mockClient.EXPECT().
				CreateAuthorizationResource(ctx, gomock.Any()).
				Return(nil, context.DeadlineExceeded)

			_, err := manager.CreateProjectAuthorizationResource(ctx, testProjectID, testTenant, testProject, testScopes)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create authorization resource"))
		})
	})

	Describe("DeleteAuthorizationResource", func() {
		It("should delete an authorization resource by ID", func() {
			resourceID := "resource-456"

			mockClient.EXPECT().
				DeleteAuthorizationResource(ctx, resourceID).
				Return(nil)

			err := manager.DeleteAuthorizationResource(ctx, resourceID)

			Expect(err).ToNot(HaveOccurred())
		})

		It("should return error when client fails", func() {
			resourceID := "resource-456"

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
