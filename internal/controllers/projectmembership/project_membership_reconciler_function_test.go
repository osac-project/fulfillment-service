/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package projectmembership

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

var _ = Describe("Finalizer Management", func() {
	It("should add finalizer on first call", func() {
		membership := privatev1.ProjectMembership_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{},
			}.Build(),
		}.Build()

		task := &task{
			membership: membership,
		}

		added := task.addFinalizer()
		Expect(added).To(BeTrue())
		Expect(membership.GetMetadata().GetFinalizers()).To(ContainElement(finalizers.ProjectMembershipFinalizer))
	})

	It("should not add finalizer if already present", func() {
		membership := privatev1.ProjectMembership_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.ProjectMembershipFinalizer},
			}.Build(),
		}.Build()

		task := &task{
			membership: membership,
		}

		added := task.addFinalizer()
		Expect(added).To(BeFalse())
		Expect(membership.GetMetadata().GetFinalizers()).To(HaveLen(1))
	})
})

var _ = Describe("Default Values", func() {
	It("should set default status if missing", func() {
		membership := privatev1.ProjectMembership_builder{}.Build()

		task := &task{
			membership: membership,
		}

		task.setDefaults()

		Expect(membership.HasStatus()).To(BeTrue())
		Expect(membership.GetStatus().GetState()).To(Equal(privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_PENDING))
	})

	It("should set default state if unspecified", func() {
		membership := privatev1.ProjectMembership_builder{
			Status: privatev1.ProjectMembershipStatus_builder{
				State: privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_UNSPECIFIED,
			}.Build(),
		}.Build()

		task := &task{
			membership: membership,
		}

		task.setDefaults()

		Expect(membership.GetStatus().GetState()).To(Equal(privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_PENDING))
	})

	It("should not change existing non-unspecified state", func() {
		membership := privatev1.ProjectMembership_builder{
			Status: privatev1.ProjectMembershipStatus_builder{
				State: privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_READY,
			}.Build(),
		}.Build()

		task := &task{
			membership: membership,
		}

		task.setDefaults()

		Expect(membership.GetStatus().GetState()).To(Equal(privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_READY))
	})
})

var _ = Describe("Role to Group Suffix Mapping", func() {
	It("should map VIEWER role to system:viewers suffix", func() {
		t := &task{}
		suffix := t.mapRoleToGroupSuffix(privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER)
		Expect(suffix).To(Equal("system:viewers"))
	})

	It("should map MANAGER role to system:managers suffix", func() {
		t := &task{}
		suffix := t.mapRoleToGroupSuffix(privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER)
		Expect(suffix).To(Equal("system:managers"))
	})

	It("should return empty string for unspecified role", func() {
		t := &task{}
		suffix := t.mapRoleToGroupSuffix(privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_UNSPECIFIED)
		Expect(suffix).To(Equal(""))
	})
})

var _ = Describe("Project Group Path Building", func() {
	var (
		ctrl                         *gomock.Controller
		mockProjectsClient           *MockProjectsClient
		mockProjectMembershipsClient *MockProjectMembershipsClient
		mockUsersClient              *MockUsersClient
		mockIdpClient                *idp.MockClientInterface
		ctx                          context.Context
		functionObj                  *function
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockProjectsClient = NewMockProjectsClient(ctrl)
		mockProjectMembershipsClient = NewMockProjectMembershipsClient(ctrl)
		mockUsersClient = NewMockUsersClient(ctrl)
		mockIdpClient = idp.NewMockClientInterface(ctrl)
		ctx = context.Background()

		functionObj = &function{
			logger:                   logger,
			projectMembershipsClient: mockProjectMembershipsClient,
			projectsClient:           mockProjectsClient,
			usersClient:              mockUsersClient,
			idpClient:                mockIdpClient,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Top-level projects (no parent)", func() {
		It("should build simple path for top-level project", func() {
			project := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-project",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			task := &task{
				r: functionObj,
			}

			path, err := task.buildProjectGroupPath(ctx, project, "system:managers")
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/my-project/system:managers"))
		})

		It("should build path with viewers suffix", func() {
			project := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-project",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			task := &task{
				r: functionObj,
			}

			path, err := task.buildProjectGroupPath(ctx, project, "system:viewers")
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/my-project/system:viewers"))
		})
	})

	Context("Nested projects (with parent)", func() {
		It("should build hierarchical path for one-level nesting", func() {
			parentProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "parent",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			childProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "child",
					Project: "parent",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: parentProject}, nil)

			task := &task{
				r: functionObj,
			}

			path, err := task.buildProjectGroupPath(ctx, childProject, "system:managers")
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/parent/child/system:managers"))
		})

		It("should build hierarchical path for two-level nesting", func() {
			rootProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "root",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			parentProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "parent",
					Project: "root",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			childProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "child",
					Project: "parent",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			// First call: get parent project
			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: parentProject}, nil)

			// Second call: get root project
			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: rootProject}, nil)

			task := &task{
				r: functionObj,
			}

			path, err := task.buildProjectGroupPath(ctx, childProject, "system:viewers")
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/root/parent/child/system:viewers"))
		})

		It("should build hierarchical path for three-level nesting", func() {
			orgProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "org",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			teamProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "team",
					Project: "org",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			productProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "product",
					Project: "team",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			componentProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "component",
					Project: "product",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			// First call: get product project
			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: productProject}, nil)

			// Second call: get team project
			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: teamProject}, nil)

			// Third call: get org project
			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: orgProject}, nil)

			task := &task{
				r: functionObj,
			}

			path, err := task.buildProjectGroupPath(ctx, componentProject, "system:managers")
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(Equal("/org/team/product/component/system:managers"))
		})
	})

	Context("Error handling", func() {
		It("should return error when parent project fetch fails", func() {
			childProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "child",
					Project: "parent",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			// getProjectByNameOrID will try Get first, then List
			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(nil, status.Error(codes.NotFound, "parent not found"))

			mockProjectsClient.EXPECT().
				List(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsListResponse{Items: []*privatev1.Project{}}, nil)

			task := &task{
				r: functionObj,
			}

			path, err := task.buildProjectGroupPath(ctx, childProject, "managers")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to fetch parent project"))
			Expect(path).To(Equal(""))
		})

		It("should detect circular reference and return error", func() {
			// Create a scenario where we hit max depth
			// This simulates a circular reference by creating a deep chain
			deepProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "level",
					Project: "parent",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			// Mock calls up to max depth
			for i := 0; i < MaxProjectHierarchyDepth; i++ {
				mockProjectsClient.EXPECT().
					Get(gomock.Any(), gomock.Any()).
					Return(&privatev1.ProjectsGetResponse{Object: deepProject}, nil)
			}

			task := &task{
				r: functionObj,
			}

			path, err := task.buildProjectGroupPath(ctx, deepProject, "system:managers")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("exceeded maximum depth"))
			Expect(err.Error()).To(ContainSubstring("circular reference"))
			Expect(path).To(Equal(""))
		})
	})
})

var _ = Describe("Synchronization", func() {
	var (
		ctrl                         *gomock.Controller
		mockProjectsClient           *MockProjectsClient
		mockProjectMembershipsClient *MockProjectMembershipsClient
		mockUsersClient              *MockUsersClient
		mockIdpClient                *idp.MockClientInterface
		ctx                          context.Context
		functionObj                  *function
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockProjectsClient = NewMockProjectsClient(ctrl)
		mockProjectMembershipsClient = NewMockProjectMembershipsClient(ctrl)
		mockUsersClient = NewMockUsersClient(ctrl)
		mockIdpClient = idp.NewMockClientInterface(ctrl)
		ctx = context.Background()

		functionObj = &function{
			logger:                   logger,
			projectMembershipsClient: mockProjectMembershipsClient,
			projectsClient:           mockProjectsClient,
			usersClient:              mockUsersClient,
			idpClient:                mockIdpClient,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Top-level project membership", func() {
		It("should add user to authorization group for top-level project", func() {
			user := privatev1.User_builder{
				Spec: privatev1.UserSpec_builder{
					Username: "alice",
				}.Build(),
				Status: privatev1.UserStatus_builder{
					KeycloakUserId: "keycloak-alice-id",
				}.Build(),
			}.Build()

			project := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "my-project",
					Tenant: "acme",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			membership := privatev1.ProjectMembership_builder{
				Spec: privatev1.ProjectMembershipSpec_builder{
					User:    new("user-id"),
					Project: "project-id",
					Role:    privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
				}.Build(),
				Status: privatev1.ProjectMembershipStatus_builder{
					State: privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_PENDING,
				}.Build(),
			}.Build()

			mockUsersClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.UsersGetResponse{Object: user}, nil)

			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: project}, nil)

			mockIdpClient.EXPECT().
				GetGroupIDByPath(gomock.Any(), "acme", "/my-project/system:managers").
				Return("group-id", nil)

			mockIdpClient.EXPECT().
				AddUserToGroup(gomock.Any(), "acme", "keycloak-alice-id", "group-id").
				Return(nil)

			task := &task{
				r:          functionObj,
				membership: membership,
			}

			err := task.syncProjectMembership(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(membership.GetStatus().GetState()).To(Equal(privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_READY))
			Expect(membership.GetStatus().GetMessage()).To(Equal(""))
		})
	})

	Context("Nested project membership", func() {
		It("should add user to hierarchical authorization group", func() {
			user := privatev1.User_builder{
				Spec: privatev1.UserSpec_builder{
					Username: "alice",
				}.Build(),
				Status: privatev1.UserStatus_builder{
					KeycloakUserId: "keycloak-alice-id",
				}.Build(),
			}.Build()

			parentProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "parent",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			childProject := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "child",
					Tenant:  "acme",
					Project: "parent",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			membership := privatev1.ProjectMembership_builder{
				Spec: privatev1.ProjectMembershipSpec_builder{
					User:    new("user-id"),
					Project: "child-project-id",
					Role:    privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER,
				}.Build(),
				Status: privatev1.ProjectMembershipStatus_builder{
					State: privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_PENDING,
				}.Build(),
			}.Build()

			mockUsersClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.UsersGetResponse{Object: user}, nil)

			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: childProject}, nil)

			// buildProjectGroupPath will fetch parent
			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: parentProject}, nil)

			mockIdpClient.EXPECT().
				GetGroupIDByPath(gomock.Any(), "acme", "/parent/child/system:viewers").
				Return("group-id", nil)

			mockIdpClient.EXPECT().
				AddUserToGroup(gomock.Any(), "acme", "keycloak-alice-id", "group-id").
				Return(nil)

			task := &task{
				r:          functionObj,
				membership: membership,
			}

			err := task.syncProjectMembership(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(membership.GetStatus().GetState()).To(Equal(privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_READY))
		})
	})

	Context("Error handling", func() {
		It("should fail when user does not exist", func() {
			membership := privatev1.ProjectMembership_builder{
				Spec: privatev1.ProjectMembershipSpec_builder{
					User:    new("nonexistent-user"),
					Project: "project-id",
					Role:    privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
				}.Build(),
				Status: privatev1.ProjectMembershipStatus_builder{
					State: privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_PENDING,
				}.Build(),
			}.Build()

			mockUsersClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(nil, status.Error(codes.NotFound, "user not found"))

			task := &task{
				r:          functionObj,
				membership: membership,
			}

			err := task.syncProjectMembership(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(membership.GetStatus().GetState()).To(Equal(privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_FAILED))
			Expect(membership.GetStatus().GetMessage()).To(ContainSubstring("Failed to fetch user"))
		})

		It("should fail when project does not exist", func() {
			user := privatev1.User_builder{
				Spec: privatev1.UserSpec_builder{
					Username: "alice",
				}.Build(),
				Status: privatev1.UserStatus_builder{
					KeycloakUserId: "keycloak-alice-id",
				}.Build(),
			}.Build()

			membership := privatev1.ProjectMembership_builder{
				Spec: privatev1.ProjectMembershipSpec_builder{
					User:    new("user-id"),
					Project: "nonexistent-project",
					Role:    privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
				}.Build(),
				Status: privatev1.ProjectMembershipStatus_builder{
					State: privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_PENDING,
				}.Build(),
			}.Build()

			mockUsersClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.UsersGetResponse{Object: user}, nil)

			// getProjectByNameOrID will try Get first, then List
			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(nil, status.Error(codes.NotFound, "project not found"))

			mockProjectsClient.EXPECT().
				List(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsListResponse{Items: []*privatev1.Project{}}, nil)

			task := &task{
				r:          functionObj,
				membership: membership,
			}

			err := task.syncProjectMembership(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(membership.GetStatus().GetState()).To(Equal(privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_FAILED))
			Expect(membership.GetStatus().GetMessage()).To(ContainSubstring("Failed to fetch project"))
		})

		It("should fail when authorization group does not exist", func() {
			user := privatev1.User_builder{
				Spec: privatev1.UserSpec_builder{
					Username: "alice",
				}.Build(),
				Status: privatev1.UserStatus_builder{
					KeycloakUserId: "keycloak-alice-id",
				}.Build(),
			}.Build()

			project := privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "my-project",
					Tenant: "acme",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{}.Build(),
			}.Build()

			membership := privatev1.ProjectMembership_builder{
				Spec: privatev1.ProjectMembershipSpec_builder{
					User:    new("user-id"),
					Project: "project-id",
					Role:    privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
				}.Build(),
				Status: privatev1.ProjectMembershipStatus_builder{
					State: privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_PENDING,
				}.Build(),
			}.Build()

			mockUsersClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.UsersGetResponse{Object: user}, nil)

			mockProjectsClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: project}, nil)

			mockIdpClient.EXPECT().
				GetGroupIDByPath(gomock.Any(), "acme", "/my-project/system:managers").
				Return("", status.Error(codes.NotFound, "group not found"))

			task := &task{
				r:          functionObj,
				membership: membership,
			}

			err := task.syncProjectMembership(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(membership.GetStatus().GetState()).To(Equal(privatev1.ProjectMembershipState_PROJECT_MEMBERSHIP_STATE_FAILED))
			Expect(membership.GetStatus().GetMessage()).To(ContainSubstring("Failed to find authorization group"))
		})
	})
})

var _ = Describe("Deletion Cleanup", func() {
	var (
		ctrl                         *gomock.Controller
		mockProjectsClient           *MockProjectsClient
		mockProjectMembershipsClient *MockProjectMembershipsClient
		mockUsersClient              *MockUsersClient
		mockIdpClient                *idp.MockClientInterface
		ctx                          context.Context
		functionObj                  *function
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockProjectsClient = NewMockProjectsClient(ctrl)
		mockProjectMembershipsClient = NewMockProjectMembershipsClient(ctrl)
		mockUsersClient = NewMockUsersClient(ctrl)
		mockIdpClient = idp.NewMockClientInterface(ctrl)
		ctx = context.Background()

		functionObj = &function{
			logger:                   logger,
			projectMembershipsClient: mockProjectMembershipsClient,
			projectsClient:           mockProjectsClient,
			usersClient:              mockUsersClient,
			idpClient:                mockIdpClient,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("should remove user from authorization group on deletion", func() {
		user := privatev1.User_builder{
			Spec: privatev1.UserSpec_builder{
				Username: "alice",
			}.Build(),
			Status: privatev1.UserStatus_builder{
				KeycloakUserId: "keycloak-alice-id",
			}.Build(),
		}.Build()

		project := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Name:   "my-project",
				Tenant: "acme",
			}.Build(),
			Spec: privatev1.ProjectSpec_builder{}.Build(),
		}.Build()

		membership := privatev1.ProjectMembership_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.ProjectMembershipFinalizer},
			}.Build(),
			Spec: privatev1.ProjectMembershipSpec_builder{
				User:    new("user-id"),
				Project: "project-id",
				Role:    privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
			}.Build(),
		}.Build()

		mockUsersClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			Return(&privatev1.UsersGetResponse{Object: user}, nil)

		mockProjectsClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsGetResponse{Object: project}, nil)

		mockIdpClient.EXPECT().
			GetGroupIDByPath(gomock.Any(), "acme", "/my-project/system:managers").
			Return("group-id", nil)

		mockIdpClient.EXPECT().
			RemoveUserFromGroup(gomock.Any(), "acme", "keycloak-alice-id", "group-id").
			Return(nil)

		task := &task{
			r:          functionObj,
			membership: membership,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(membership.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.ProjectMembershipFinalizer))
	})

	It("should remove user from nested project authorization group on deletion", func() {
		user := privatev1.User_builder{
			Spec: privatev1.UserSpec_builder{
				Username: "alice",
			}.Build(),
			Status: privatev1.UserStatus_builder{
				KeycloakUserId: "keycloak-alice-id",
			}.Build(),
		}.Build()

		parentProject := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Name: "parent",
			}.Build(),
			Spec: privatev1.ProjectSpec_builder{}.Build(),
		}.Build()

		childProject := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Name:    "child",
				Tenant:  "acme",
				Project: "parent",
			}.Build(),
			Spec: privatev1.ProjectSpec_builder{}.Build(),
		}.Build()

		membership := privatev1.ProjectMembership_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.ProjectMembershipFinalizer},
			}.Build(),
			Spec: privatev1.ProjectMembershipSpec_builder{
				User:    new("user-id"),
				Project: "child-project-id",
				Role:    privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER,
			}.Build(),
		}.Build()

		mockUsersClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			Return(&privatev1.UsersGetResponse{Object: user}, nil)

		mockProjectsClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsGetResponse{Object: childProject}, nil)

		// buildProjectGroupPath will fetch parent
		mockProjectsClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsGetResponse{Object: parentProject}, nil)

		mockIdpClient.EXPECT().
			GetGroupIDByPath(gomock.Any(), "acme", "/parent/child/system:viewers").
			Return("group-id", nil)

		mockIdpClient.EXPECT().
			RemoveUserFromGroup(gomock.Any(), "acme", "keycloak-alice-id", "group-id").
			Return(nil)

		task := &task{
			r:          functionObj,
			membership: membership,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(membership.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.ProjectMembershipFinalizer))
	})

	It("should remove finalizer even if cleanup fails", func() {
		membership := privatev1.ProjectMembership_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.ProjectMembershipFinalizer},
			}.Build(),
			Spec: privatev1.ProjectMembershipSpec_builder{
				User:    new("user-id"),
				Project: "project-id",
				Role:    privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
			}.Build(),
		}.Build()

		mockUsersClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			Return(nil, status.Error(codes.NotFound, "user not found"))

		task := &task{
			r:          functionObj,
			membership: membership,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(membership.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.ProjectMembershipFinalizer))
	})

	It("should handle group not found during cleanup with gRPC status code", func() {
		user := privatev1.User_builder{
			Spec: privatev1.UserSpec_builder{
				Username: "alice",
			}.Build(),
			Status: privatev1.UserStatus_builder{
				KeycloakUserId: "keycloak-alice-id",
			}.Build(),
		}.Build()

		project := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Name:   "my-project",
				Tenant: "acme",
			}.Build(),
			Spec: privatev1.ProjectSpec_builder{}.Build(),
		}.Build()

		membership := privatev1.ProjectMembership_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.ProjectMembershipFinalizer},
			}.Build(),
			Spec: privatev1.ProjectMembershipSpec_builder{
				User:    new("user-id"),
				Project: "project-id",
				Role:    privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
			}.Build(),
		}.Build()

		mockUsersClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			Return(&privatev1.UsersGetResponse{Object: user}, nil)

		mockProjectsClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsGetResponse{Object: project}, nil)

		mockIdpClient.EXPECT().
			GetGroupIDByPath(gomock.Any(), "acme", "/my-project/system:managers").
			Return("", status.Error(codes.NotFound, "group not found"))

		task := &task{
			r:          functionObj,
			membership: membership,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(membership.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.ProjectMembershipFinalizer))
	})

	It("should handle group not found during cleanup with wrapped error message", func() {
		user := privatev1.User_builder{
			Spec: privatev1.UserSpec_builder{
				Username: "alice",
			}.Build(),
			Status: privatev1.UserStatus_builder{
				KeycloakUserId: "keycloak-alice-id",
			}.Build(),
		}.Build()

		project := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Name:   "my-project",
				Tenant: "acme",
			}.Build(),
			Spec: privatev1.ProjectSpec_builder{}.Build(),
		}.Build()

		membership := privatev1.ProjectMembership_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.ProjectMembershipFinalizer},
			}.Build(),
			Spec: privatev1.ProjectMembershipSpec_builder{
				User:    new("user-id"),
				Project: "project-id",
				Role:    privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER,
			}.Build(),
		}.Build()

		mockUsersClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			Return(&privatev1.UsersGetResponse{Object: user}, nil)

		mockProjectsClient.EXPECT().
			Get(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsGetResponse{Object: project}, nil)

		// Simulate a wrapped error that doesn't have gRPC status code but contains "not found" in message
		mockIdpClient.EXPECT().
			GetGroupIDByPath(gomock.Any(), "acme", "/my-project/system:viewers").
			Return("", fmt.Errorf("wrapped error: group not found in keycloak"))

		task := &task{
			r:          functionObj,
			membership: membership,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(membership.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.ProjectMembershipFinalizer))
	})
})
