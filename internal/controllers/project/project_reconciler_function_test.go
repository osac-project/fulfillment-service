/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package project

import (
	"context"

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
		project := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{},
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		added := task.addFinalizer()
		Expect(added).To(BeTrue())
		Expect(project.GetMetadata().GetFinalizers()).To(ContainElement(finalizers.Controller))
	})

	It("should not add finalizer if already present", func() {
		project := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		added := task.addFinalizer()
		Expect(added).To(BeFalse())
		Expect(project.GetMetadata().GetFinalizers()).To(HaveLen(1))
	})
})

var _ = Describe("Default Values", func() {
	It("should set default status if missing", func() {
		project := privatev1.Project_builder{}.Build()

		task := &task{
			project: project,
		}

		task.setDefaults()

		Expect(project.HasStatus()).To(BeTrue())
		Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_PENDING))
	})

	It("should set default state if unspecified", func() {
		project := privatev1.Project_builder{
			Status: privatev1.ProjectStatus_builder{
				State: privatev1.ProjectState_PROJECT_STATE_UNSPECIFIED,
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		task.setDefaults()

		Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_PENDING))
	})

	It("should not change existing non-unspecified state", func() {
		project := privatev1.Project_builder{
			Status: privatev1.ProjectStatus_builder{
				State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		task.setDefaults()

		Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_ACTIVE))
	})
})

var _ = Describe("Finalizer Removal", func() {
	It("should remove finalizer when present", func() {
		project := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller, "other-finalizer"},
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		task.removeFinalizer()

		Expect(project.GetMetadata().GetFinalizers()).To(ConsistOf("other-finalizer"))
		Expect(project.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should not error when finalizer not present", func() {
		project := privatev1.Project_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{"other-finalizer"},
			}.Build(),
		}.Build()

		task := &task{
			project: project,
		}

		task.removeFinalizer()

		Expect(project.GetMetadata().GetFinalizers()).To(ConsistOf("other-finalizer"))
	})

	It("should handle missing metadata", func() {
		project := privatev1.Project_builder{}.Build()

		task := &task{
			project: project,
		}

		// Should not panic
		task.removeFinalizer()
	})
})

var _ = Describe("Validation and Activation", func() {
	var (
		ctrl            *gomock.Controller
		mockClient      *MockProjectsClient
		mockIdpClient   *idp.MockClient
		resourceManager *idp.ResourceManager
		ctx             context.Context
		functionObj     *function
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = NewMockProjectsClient(ctrl)
		mockIdpClient = idp.NewMockClient(ctrl)
		ctx = context.Background()

		var err error
		resourceManager, err = idp.NewResourceManager().
			SetLogger(logger).
			SetClient(mockIdpClient).
			Build()
		Expect(err).ToNot(HaveOccurred())

		functionObj = &function{
			logger:          logger,
			projectsClient:  mockClient,
			resourceManager: resourceManager,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Top-level projects (no parent)", func() {
		It("should transition to ACTIVE state", func() {
			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name:   "test-project",
					Tenant: "acme",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Test Project",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			// Expect authorization resource creation
			mockIdpClient.EXPECT().
				CreateAuthorizationResource(gomock.Any(), gomock.Any()).
				Return(&idp.AuthorizationResource{
					ID:   "resource-123",
					Name: "PROJECT-acme-test-project",
				}, nil)

			// Expect viewers group creation (new organization groups API)
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "acme", "viewers", "/test-project/viewers").
				Return(nil)

			// Expect managers group creation (new organization groups API)
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "acme", "managers", "/test-project/managers").
				Return(nil)

			task := &task{
				r:       functionObj,
				project: project,
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_ACTIVE))
			Expect(project.GetStatus().HasMessage()).To(BeFalse())
		})

		It("should store Keycloak resource ID in status", func() {
			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name:   "test-project",
					Tenant: "acme",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Test Project",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			// Expect authorization resource creation
			mockIdpClient.EXPECT().
				CreateAuthorizationResource(gomock.Any(), gomock.Any()).
				Return(&idp.AuthorizationResource{
					ID:   "resource-abc-123",
					Name: "PROJECT-acme-test-project",
				}, nil)

			// Expect viewers group creation (new organization groups API)
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "acme", "viewers", "/test-project/viewers").
				Return(nil)

			// Expect managers group creation (new organization groups API)
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "acme", "managers", "/test-project/managers").
				Return(nil)

			task := &task{
				r:       functionObj,
				project: project,
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			// Verify resource ID is stored in status
			Expect(project.GetStatus().HasKeycloakResourceId()).To(BeTrue())
			Expect(project.GetStatus().GetKeycloakResourceId()).To(Equal("resource-abc-123"))
		})
	})

	Context("Projects with valid parent", func() {
		It("should transition to ACTIVE when parent exists and is ACTIVE", func() {
			parentProject := privatev1.Project_builder{
				Id: "parent-1",
				Metadata: privatev1.Metadata_builder{
					Name: "parent-project",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Parent Project",
				}.Build(),
			}.Build()

			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name:    "parent-project.child-project",
					Project: "parent-project",
					Tenant:  "acme",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Child Project",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			mockClient.EXPECT().
				List(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsListResponse{
					Items: []*privatev1.Project{parentProject},
					Size:  1,
				}, nil)

			mockIdpClient.EXPECT().
				CreateAuthorizationResource(gomock.Any(), gomock.Any()).
				Return(&idp.AuthorizationResource{
					ID:   "resource-456",
					Name: "PROJECT-acme-child-project",
				}, nil)

			// Expect viewers group creation (new organization groups API)
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(
					gomock.Any(),
					"acme",
					"viewers",
					"/parent-project.child-project/viewers",
				).
				Return(nil)

			// Expect managers group creation (new organization groups API)
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(
					gomock.Any(),
					"acme",
					"managers",
					"/parent-project.child-project/managers",
				).
				Return(nil)

			task := &task{
				r:       functionObj,
				project: project,
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_ACTIVE))
		})

		It("should handle multi-level hierarchy", func() {
			rootProject := privatev1.Project_builder{
				Id: "root-id",
				Metadata: privatev1.Metadata_builder{
					Name: "root",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Root",
				}.Build(),
			}.Build()

			parentProject := privatev1.Project_builder{
				Id: "parent-id",
				Metadata: privatev1.Metadata_builder{
					Name:    "root.parent",
					Project: "root",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Parent",
				}.Build(),
			}.Build()

			project := privatev1.Project_builder{
				Id: "child-id",
				Metadata: privatev1.Metadata_builder{
					Name:    "root.parent.child",
					Project: "root.parent",
					Tenant:  "acme",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Child",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			// First List: find parent "root.parent"
			mockClient.EXPECT().
				List(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsListResponse{
					Items: []*privatev1.Project{parentProject},
					Size:  1,
				}, nil)

			// Second List: circular check finds grandparent "root"
			mockClient.EXPECT().
				List(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsListResponse{
					Items: []*privatev1.Project{rootProject},
					Size:  1,
				}, nil)

			mockIdpClient.EXPECT().
				CreateAuthorizationResource(gomock.Any(), gomock.Any()).
				Return(&idp.AuthorizationResource{
					ID:   "resource-789",
					Name: "PROJECT-acme-child-project",
				}, nil)

			// Expect viewers group creation (new organization groups API)
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(
					gomock.Any(),
					"acme",
					"viewers",
					"/root.parent.child/viewers",
				).
				Return(nil)

			// Expect managers group creation (new organization groups API)
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(
					gomock.Any(),
					"acme",
					"managers",
					"/root.parent.child/managers",
				).
				Return(nil)

			task := &task{
				r:       functionObj,
				project: project,
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_ACTIVE))
		})
	})

	Context("Self-reference validation", func() {
		It("should fail when project references itself as parent", func() {
			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name:    "my-project",
					Project: "my-project",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Self-referencing",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			task := &task{
				r:       functionObj,
				project: project,
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_FAILED))
			Expect(project.GetStatus().GetMessage()).To(Equal("Project cannot be its own parent"))
		})
	})

	Context("Parent not found", func() {
		It("should fail when parent does not exist", func() {
			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name:    "nonexistent-parent.child",
					Project: "nonexistent-parent",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Orphaned Project",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			mockClient.EXPECT().
				List(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsListResponse{
					Items: []*privatev1.Project{},
					Size:  0,
				}, nil)

			task := &task{
				r:       functionObj,
				project: project,
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_FAILED))
			Expect(project.GetStatus().GetMessage()).To(ContainSubstring("Parent project not found"))
		})
	})

	Context("Parent state validation", func() {
		It("should fail when parent is in PENDING state", func() {
			parentProject := privatev1.Project_builder{
				Id: "parent-1",
				Metadata: privatev1.Metadata_builder{
					Name: "my-parent",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Pending Parent",
				}.Build(),
			}.Build()

			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name:    "my-parent.child",
					Project: "my-parent",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Child",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			mockClient.EXPECT().
				List(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsListResponse{
					Items: []*privatev1.Project{parentProject},
					Size:  1,
				}, nil)

			task := &task{
				r:       functionObj,
				project: project,
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_FAILED))
			Expect(project.GetStatus().GetMessage()).To(ContainSubstring(
				"Parent project 'my-parent' is not in ACTIVE state (current state: PROJECT_STATE_PENDING)",
			))
		})

		It("should fail when parent is in FAILED state", func() {
			parentProject := privatev1.Project_builder{
				Id: "parent-1",
				Metadata: privatev1.Metadata_builder{
					Name: "my-parent",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_FAILED,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Failed Parent",
				}.Build(),
			}.Build()

			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name:    "my-parent.child",
					Project: "my-parent",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Child",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			mockClient.EXPECT().
				List(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsListResponse{
					Items: []*privatev1.Project{parentProject},
					Size:  1,
				}, nil)

			task := &task{
				r:       functionObj,
				project: project,
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_FAILED))
			Expect(project.GetStatus().GetMessage()).To(ContainSubstring(
				"Parent project 'my-parent' is not in ACTIVE state (current state: PROJECT_STATE_FAILED)",
			))
		})
	})

	Context("Circular dependency detection", func() {
		It("should detect direct circular dependency", func() {
			projectA := privatev1.Project_builder{
				Id: "project-a-id",
				Metadata: privatev1.Metadata_builder{
					Name:    "project-a",
					Project: "project-b",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Project A",
				}.Build(),
			}.Build()

			// List to find parent "project-a"
			mockClient.EXPECT().
				List(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsListResponse{
					Items: []*privatev1.Project{projectA},
					Size:  1,
				}, nil)

			task := &task{
				r: functionObj,
				project: privatev1.Project_builder{
					Id: "project-b-id",
					Metadata: privatev1.Metadata_builder{
						Name:    "project-b",
						Project: "project-a",
					}.Build(),
					Spec: privatev1.ProjectSpec_builder{
						Title: "Project B",
					}.Build(),
					Status: privatev1.ProjectStatus_builder{
						State: privatev1.ProjectState_PROJECT_STATE_PENDING,
					}.Build(),
				}.Build(),
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(task.project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_FAILED))
			Expect(task.project.GetStatus().GetMessage()).To(ContainSubstring("Circular dependency detected"))
		})

		It("should detect indirect circular dependency", func() {
			projectA := privatev1.Project_builder{
				Id: "project-a-id",
				Metadata: privatev1.Metadata_builder{
					Name:    "project-a",
					Project: "project-c",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Project A",
				}.Build(),
			}.Build()

			projectB := privatev1.Project_builder{
				Id: "project-b-id",
				Metadata: privatev1.Metadata_builder{
					Name:    "project-b",
					Project: "project-a",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Project B",
				}.Build(),
			}.Build()

			// First List: find parent "project-b"
			mockClient.EXPECT().
				List(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsListResponse{
					Items: []*privatev1.Project{projectB},
					Size:  1,
				}, nil)

			// Second List: circular check finds "project-a"
			mockClient.EXPECT().
				List(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsListResponse{
					Items: []*privatev1.Project{projectA},
					Size:  1,
				}, nil)

			task := &task{
				r: functionObj,
				project: privatev1.Project_builder{
					Id: "project-c-id",
					Metadata: privatev1.Metadata_builder{
						Name:    "project-c",
						Project: "project-b",
					}.Build(),
					Spec: privatev1.ProjectSpec_builder{
						Title: "Project C",
					}.Build(),
					Status: privatev1.ProjectStatus_builder{
						State: privatev1.ProjectState_PROJECT_STATE_PENDING,
					}.Build(),
				}.Build(),
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(task.project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_FAILED))
			Expect(task.project.GetStatus().GetMessage()).To(ContainSubstring("Circular dependency detected"))
		})
	})

	Context("Update skips validation", func() {
		It("should skip validation when project is already ACTIVE", func() {
			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Finalizers: []string{finalizers.Controller},
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Active Project",
				}.Build(),
			}.Build()

			task := &task{
				r:       functionObj,
				project: project,
			}

			// Should not call any client methods since validation is skipped
			err := task.update(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_ACTIVE))
		})

		It("should skip validation when project is in FAILED state", func() {
			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Finalizers: []string{finalizers.Controller},
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State:   privatev1.ProjectState_PROJECT_STATE_FAILED,
					Message: new("Some error"),
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Failed Project",
				}.Build(),
			}.Build()

			task := &task{
				r:       functionObj,
				project: project,
			}

			// Should not call any client methods since validation is skipped
			err := task.update(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_FAILED))
		})
	})
})

var _ = Describe("Deletion Cleanup", func() {
	var (
		ctrl            *gomock.Controller
		mockClient      *MockProjectsClient
		mockIdpClient   *idp.MockClient
		resourceManager *idp.ResourceManager
		ctx             context.Context
		functionObj     *function
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = NewMockProjectsClient(ctrl)
		mockIdpClient = idp.NewMockClient(ctrl)
		ctx = context.Background()

		var err error
		resourceManager, err = idp.NewResourceManager().
			SetLogger(logger).
			SetClient(mockIdpClient).
			Build()
		Expect(err).ToNot(HaveOccurred())

		functionObj = &function{
			logger:          logger,
			projectsClient:  mockClient,
			resourceManager: resourceManager,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("should block deletion when child projects exist", func() {
		project := privatev1.Project_builder{
			Id: "parent-1",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
			Status: privatev1.ProjectStatus_builder{}.Build(),
		}.Build()

		// Expect query for children
		mockClient.EXPECT().
			List(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsListResponse{
				Total: 2, // Has 2 children
			}, nil)

		task := &task{
			r:       functionObj,
			project: project,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		// State should be DELETE_FAILED
		Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_DELETE_FAILED))
		Expect(project.GetStatus().GetMessage()).To(ContainSubstring("Cannot delete project with 2 child project(s)"))
		// Finalizer should NOT be removed
		Expect(project.GetMetadata().GetFinalizers()).To(ContainElement(finalizers.Controller))
	})

	It("should delete Keycloak authorization resource when no children exist", func() {
		project := privatev1.Project_builder{
			Id: "project-1",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
			Status: privatev1.ProjectStatus_builder{
				KeycloakResourceId: new("resource-123"),
			}.Build(),
		}.Build()

		// Expect query for children (returns 0)
		mockClient.EXPECT().
			List(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsListResponse{
				Size: 0,
			}, nil)

		// Expect get resource to extract name and tenant for group cleanup
		mockIdpClient.EXPECT().
			GetAuthorizationResource(gomock.Any(), "resource-123").
			Return(&idp.AuthorizationResource{
				ID:   "resource-123",
				Name: "PROJECT-acme-test-project",
				Attributes: map[string][]string{
					"tenant": {"acme"},
				},
			}, nil)

		// Expect viewers group ID lookup (new organization groups API with new paths)
		mockIdpClient.EXPECT().
			GetGroupIDByPath(gomock.Any(), "acme", "/test-project/viewers").
			Return("viewers-group-id", nil)

		// Expect viewers group deletion (new organization groups API)
		mockIdpClient.EXPECT().
			DeleteAuthorizationGroup(gomock.Any(), "acme", "viewers-group-id").
			Return(nil)

		// Expect managers group ID lookup (new organization groups API with new paths)
		mockIdpClient.EXPECT().
			GetGroupIDByPath(gomock.Any(), "acme", "/test-project/managers").
			Return("managers-group-id", nil)

		// Expect managers group deletion (new organization groups API)
		mockIdpClient.EXPECT().
			DeleteAuthorizationGroup(gomock.Any(), "acme", "managers-group-id").
			Return(nil)

		// Expect resource deletion
		mockIdpClient.EXPECT().
			DeleteAuthorizationResource(gomock.Any(), "resource-123").
			Return(nil)

		task := &task{
			r:       functionObj,
			project: project,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(project.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should remove finalizer even if Keycloak deletion fails", func() {
		project := privatev1.Project_builder{
			Id: "project-1",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
			Status: privatev1.ProjectStatus_builder{
				KeycloakResourceId: new("resource-456"),
			}.Build(),
		}.Build()

		// Expect query for children (returns 0)
		mockClient.EXPECT().
			List(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsListResponse{
				Size: 0,
			}, nil)

		// Expect get resource to fail (resource already deleted)
		mockIdpClient.EXPECT().
			GetAuthorizationResource(gomock.Any(), "resource-456").
			Return(nil, status.Error(codes.NotFound, "resource not found"))

		// Expect resource deletion call that also fails
		mockIdpClient.EXPECT().
			DeleteAuthorizationResource(gomock.Any(), "resource-456").
			Return(status.Error(codes.NotFound, "resource not found"))

		task := &task{
			r:       functionObj,
			project: project,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		// Finalizer should still be removed even though Keycloak deletion failed
		Expect(project.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
		// Condition should be updated to reflect failure
		conditions := project.GetStatus().GetConditions()
		var keycloakCondition *privatev1.ProjectCondition
		for _, cond := range conditions {
			if cond.GetType() == privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_KEYCLOAK_SYNC {
				keycloakCondition = cond
				break
			}
		}
		Expect(keycloakCondition).ToNot(BeNil())
		Expect(keycloakCondition.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_FALSE))
		Expect(keycloakCondition.GetReason()).To(Equal("DeletionFailed"))
	})

	It("should skip Keycloak deletion if no resource ID annotation exists", func() {
		project := privatev1.Project_builder{
			Id: "project-1",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
		}.Build()

		// Expect query for children (returns 0)
		mockClient.EXPECT().
			List(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsListResponse{
				Size: 0,
			}, nil)

		// No IDP client calls expected

		task := &task{
			r:       functionObj,
			project: project,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(project.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should skip Keycloak deletion if resource ID is not set", func() {
		project := privatev1.Project_builder{
			Id: "project-1",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
			Status: privatev1.ProjectStatus_builder{}.Build(),
		}.Build()

		// Expect query for children (returns 0)
		mockClient.EXPECT().
			List(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsListResponse{
				Size: 0,
			}, nil)

		// No IDP client calls expected

		task := &task{
			r:       functionObj,
			project: project,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(project.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should return error if querying for children fails", func() {
		project := privatev1.Project_builder{
			Id: "project-1",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
		}.Build()

		// Expect query for children to fail
		mockClient.EXPECT().
			List(gomock.Any(), gomock.Any()).
			Return(nil, status.Error(codes.Unavailable, "database unavailable"))

		task := &task{
			r:       functionObj,
			project: project,
		}

		err := task.delete(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to query for child projects"))
		// Finalizer should NOT be removed on error
		Expect(project.GetMetadata().GetFinalizers()).To(ContainElement(finalizers.Controller))
	})
})
