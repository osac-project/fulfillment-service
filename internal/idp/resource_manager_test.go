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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("ResourceManager", func() {
	var (
		ctrl       *gomock.Controller
		mockClient *MockClient
		ctx        = context.Background()
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = NewMockClient(ctrl)
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

	Describe("DeleteProjectGroups", func() {
		var manager *ResourceManager

		BeforeEach(func() {
			var err error
			manager, err = NewResourceManager().
				SetLogger(logger).
				SetClient(mockClient).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should delete parent project group only (cascade delete)", func() {
			mockClient.EXPECT().
				GetGroupIDByPath(gomock.Any(), "test-org", "/test-project").
				Return("group-id-123", nil)

			mockClient.EXPECT().
				DeleteAuthorizationGroup(gomock.Any(), "test-org", "group-id-123").
				Return(nil)

			err := manager.DeleteProjectGroups(ctx, "test-org", "test-project")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return nil when parent group is not found", func() {
			mockClient.EXPECT().
				GetGroupIDByPath(gomock.Any(), "test-org", "/test-project").
				Return("", errors.New("organization group not found: /test-project"))

			err := manager.DeleteProjectGroups(ctx, "test-org", "test-project")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should propagate non-not-found errors from GetGroupIDByPath", func() {
			mockClient.EXPECT().
				GetGroupIDByPath(gomock.Any(), "test-org", "/test-project").
				Return("", errors.New("network error: connection timeout"))

			err := manager.DeleteProjectGroups(ctx, "test-org", "test-project")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get project group ID"))
			Expect(err.Error()).To(ContainSubstring("network error"))
		})

		It("should return error when deletion fails", func() {
			mockClient.EXPECT().
				GetGroupIDByPath(gomock.Any(), "test-org", "/test-project").
				Return("group-id-123", nil)

			mockClient.EXPECT().
				DeleteAuthorizationGroup(gomock.Any(), "test-org", "group-id-123").
				Return(errors.New("keycloak error"))

			err := manager.DeleteProjectGroups(ctx, "test-org", "test-project")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to delete project group"))
		})

		It("should return error when tenant is empty", func() {
			err := manager.DeleteProjectGroups(ctx, "", "test-project")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("tenant is required"))
		})

		It("should return error when project name is empty", func() {
			err := manager.DeleteProjectGroups(ctx, "test-org", "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("project name is required"))
		})
	})

	Describe("CreateProjectGroups", func() {
		var manager *ResourceManager

		BeforeEach(func() {
			var err error
			manager, err = NewResourceManager().
				SetLogger(logger).
				SetClient(mockClient).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should create hierarchical viewers and managers groups", func() {
			mockClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "test-org", GroupNameViewers, "/test-project/viewers").
				Return(nil)

			mockClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "test-org", GroupNameManagers, "/test-project/managers").
				Return(nil)

			err := manager.CreateProjectGroups(ctx, "test-org", "test-project")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should rollback viewers group when managers group creation fails", func() {
			mockClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "test-org", GroupNameViewers, "/test-project/viewers").
				Return(nil)

			mockClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "test-org", GroupNameManagers, "/test-project/managers").
				Return(errors.New("keycloak error"))

			mockClient.EXPECT().
				GetGroupIDByPath(gomock.Any(), "test-org", "/test-project/viewers").
				Return("viewers-group-id", nil)

			mockClient.EXPECT().
				DeleteAuthorizationGroup(gomock.Any(), "test-org", "viewers-group-id").
				Return(nil)

			err := manager.CreateProjectGroups(ctx, "test-org", "test-project")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create managers group"))
		})

		It("should return error when viewers group creation fails", func() {
			mockClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "test-org", GroupNameViewers, "/test-project/viewers").
				Return(errors.New("keycloak error"))

			err := manager.CreateProjectGroups(ctx, "test-org", "test-project")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create viewers group"))
		})
	})
})
