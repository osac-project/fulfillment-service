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

			// Expect viewers group creation
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "acme", "viewers", "/test-project/viewers").
				Return(nil)

			// Expect managers group creation
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

		It("should set Keycloak sync condition to true on success", func() {
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

			// Expect viewers group creation
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "acme", "viewers", "/test-project/viewers").
				Return(nil)

			// Expect managers group creation
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "acme", "managers", "/test-project/managers").
				Return(nil)

			task := &task{
				r:       functionObj,
				project: project,
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify Keycloak sync condition is set to true
			var syncCondition *privatev1.ProjectCondition
			for _, cond := range project.GetStatus().GetConditions() {
				if cond.GetType() == privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_KEYCLOAK_SYNC {
					syncCondition = cond
					break
				}
			}
			Expect(syncCondition).ToNot(BeNil())
			Expect(syncCondition.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_TRUE))
			Expect(syncCondition.GetReason()).To(Equal("GroupsCreated"))
		})
	})

	Context("Projects with valid parent", func() {
		It("should transition to ACTIVE when parent exists and is ACTIVE", func() {
			parentProject := privatev1.Project_builder{
				Id: "parent-1",
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
					Name:   "child-project",
					Tenant: "acme",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Parent: new("parent-1"),
					Title:  "Child Project",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			// Single call: validate immediate parent (no grandparent, so no circular check fetch needed)
			mockClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: parentProject}, nil)

			// Expect viewers group creation
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "acme", "viewers", "/child-project/viewers").
				Return(nil)

			// Expect managers group creation
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "acme", "managers", "/child-project/managers").
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
				Id: "root",
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Root",
				}.Build(),
			}.Build()

			parentProject := privatev1.Project_builder{
				Id: "parent",
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Parent: new("root"),
					Title:  "Parent",
				}.Build(),
			}.Build()

			project := privatev1.Project_builder{
				Id: "child",
				Metadata: privatev1.Metadata_builder{
					Name:   "child-project",
					Tenant: "acme",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Parent: new("parent"),
					Title:  "Child",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			// First call: Get immediate parent
			mockClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: parentProject}, nil)

			// Second call: Get grandparent during circular dependency check
			mockClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: rootProject}, nil)

			// Expect viewers group creation
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "acme", "viewers", "/child-project/viewers").
				Return(nil)

			// Expect managers group creation (new organization groups API)
			mockIdpClient.EXPECT().
				CreateAuthorizationGroup(gomock.Any(), "acme", "managers", "/child-project/managers").
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
				Spec: privatev1.ProjectSpec_builder{
					Parent: new("project-1"),
					Title:  "Self-referencing",
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
				Spec: privatev1.ProjectSpec_builder{
					Parent: new("nonexistent-parent"),
					Title:  "Orphaned Project",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			mockClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(nil, status.Error(codes.NotFound, "not found"))

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
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Pending Parent",
				}.Build(),
			}.Build()

			project := privatev1.Project_builder{
				Id: "project-1",
				Spec: privatev1.ProjectSpec_builder{
					Parent: new("parent-1"),
					Title:  "Child",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			mockClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: parentProject}, nil)

			task := &task{
				r:       functionObj,
				project: project,
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_FAILED))
			Expect(project.GetStatus().GetMessage()).To(ContainSubstring("Parent project parent-1 is not in ACTIVE state (current state: PROJECT_STATE_PENDING)"))
		})

		It("should fail when parent is in FAILED state", func() {
			parentProject := privatev1.Project_builder{
				Id: "parent-1",
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_FAILED,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Failed Parent",
				}.Build(),
			}.Build()

			project := privatev1.Project_builder{
				Id: "project-1",
				Spec: privatev1.ProjectSpec_builder{
					Parent: new("parent-1"),
					Title:  "Child",
				}.Build(),
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_PENDING,
				}.Build(),
			}.Build()

			mockClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: parentProject}, nil)

			task := &task{
				r:       functionObj,
				project: project,
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_FAILED))
			Expect(project.GetStatus().GetMessage()).To(ContainSubstring("Parent project parent-1 is not in ACTIVE state (current state: PROJECT_STATE_FAILED)"))
		})
	})

	Context("Circular dependency detection", func() {
		It("should detect direct circular dependency", func() {
			projectA := privatev1.Project_builder{
				Id: "project-a",
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Parent: new("project-b"),
					Title:  "Project A",
				}.Build(),
			}.Build()

			// Single call: Get project A (immediate parent)
			// Circular check detects A→B cycle without additional fetch (B is the project being validated)
			mockClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: projectA}, nil)

			task := &task{
				r: functionObj,
				project: privatev1.Project_builder{
					Id: "project-b",
					Spec: privatev1.ProjectSpec_builder{
						Parent: new("project-a"),
						Title:  "Project B",
					}.Build(),
					Status: privatev1.ProjectStatus_builder{
						State: privatev1.ProjectState_PROJECT_STATE_PENDING,
					}.Build(),
				}.Build(),
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(task.project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_FAILED))
			Expect(task.project.GetStatus().GetMessage()).To(ContainSubstring("circular dependency detected"))
		})

		It("should detect indirect circular dependency", func() {
			projectA := privatev1.Project_builder{
				Id: "project-a",
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Parent: new("project-c"),
					Title:  "Project A",
				}.Build(),
			}.Build()

			projectB := privatev1.Project_builder{
				Id: "project-b",
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Parent: new("project-a"),
					Title:  "Project B",
				}.Build(),
			}.Build()

			// Get immediate parent (B) to validate it exists and is ACTIVE
			mockClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: projectB}, nil)

			// Circular check: Get A (B is already fetched, so we traverse to A next)
			mockClient.EXPECT().
				Get(gomock.Any(), gomock.Any()).
				Return(&privatev1.ProjectsGetResponse{Object: projectA}, nil)

			task := &task{
				r: functionObj,
				project: privatev1.Project_builder{
					Id: "project-c",
					Spec: privatev1.ProjectSpec_builder{
						Parent: new("project-b"),
						Title:  "Project C",
					}.Build(),
					Status: privatev1.ProjectStatus_builder{
						State: privatev1.ProjectState_PROJECT_STATE_PENDING,
					}.Build(),
				}.Build(),
			}

			err := task.validateAndActivate(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(task.project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_FAILED))
			Expect(task.project.GetStatus().GetMessage()).To(ContainSubstring("circular dependency detected"))
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
				Size: 2, // Has 2 children
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

	It("should delete Keycloak groups when no children exist", func() {
		project := privatev1.Project_builder{
			Id: "project-1",
			Metadata: privatev1.Metadata_builder{
				Name:       "test-project",
				Tenant:     "acme",
				Finalizers: []string{finalizers.Controller},
			}.Build(),
		}.Build()

		// Expect query for children (returns 0)
		mockClient.EXPECT().
			List(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsListResponse{
				Size: 0,
			}, nil)

		// Expect parent project group ID lookup
		mockIdpClient.EXPECT().
			GetGroupIDByPath(gomock.Any(), "acme", "/test-project").
			Return("project-group-id", nil)

		// Expect parent project group deletion (cascades to delete viewers and managers subgroups)
		mockIdpClient.EXPECT().
			DeleteAuthorizationGroup(gomock.Any(), "acme", "project-group-id").
			Return(nil)

		task := &task{
			r:       functionObj,
			project: project,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(project.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should remove finalizer even if Keycloak group deletion fails", func() {
		project := privatev1.Project_builder{
			Id: "project-1",
			Metadata: privatev1.Metadata_builder{
				Name:       "test-project",
				Tenant:     "acme",
				Finalizers: []string{finalizers.Controller},
			}.Build(),
		}.Build()

		// Expect query for children (returns 0)
		mockClient.EXPECT().
			List(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsListResponse{
				Size: 0,
			}, nil)

		// Expect parent project group ID lookup to fail
		mockIdpClient.EXPECT().
			GetGroupIDByPath(gomock.Any(), "acme", "/test-project").
			Return("", status.Error(codes.NotFound, "group not found"))

		task := &task{
			r:       functionObj,
			project: project,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		// Finalizer should still be removed even though Keycloak deletion failed
		Expect(project.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should handle missing tenant gracefully during deletion", func() {
		project := privatev1.Project_builder{
			Id: "project-1",
			Metadata: privatev1.Metadata_builder{
				Name:       "test-project",
				Finalizers: []string{finalizers.Controller},
				// Missing tenant
			}.Build(),
		}.Build()

		// Expect query for children (returns 0)
		mockClient.EXPECT().
			List(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsListResponse{
				Size: 0,
			}, nil)

		// No IDP client calls expected - DeleteProjectGroups will return error for missing tenant
		// but deletion continues

		task := &task{
			r:       functionObj,
			project: project,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(project.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should handle missing project name gracefully during deletion", func() {
		project := privatev1.Project_builder{
			Id: "project-1",
			Metadata: privatev1.Metadata_builder{
				Tenant:     "acme",
				Finalizers: []string{finalizers.Controller},
				// Missing name
			}.Build(),
		}.Build()

		// Expect query for children (returns 0)
		mockClient.EXPECT().
			List(gomock.Any(), gomock.Any()).
			Return(&privatev1.ProjectsListResponse{
				Size: 0,
			}, nil)

		// No IDP client calls expected - DeleteProjectGroups will return error for missing name
		// but deletion continues

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
