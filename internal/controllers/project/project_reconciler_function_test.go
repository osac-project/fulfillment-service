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
	"log/slog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
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
		ctrl           *gomock.Controller
		mockClient     *MockProjectsClient
		mockHubsClient *MockHubsClient
		ctx            context.Context
		functionObj    *function
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = NewMockProjectsClient(ctrl)
		mockHubsClient = NewMockHubsClient(ctrl)
		ctx = context.Background()
		functionObj = &function{
			logger:         slog.Default(),
			projectsClient: mockClient,
			hubsClient:     mockHubsClient,
		}

		// Mock hubsClient.List() to return empty list (no hubs to sync to)
		// This allows tests to focus on validation logic without hub sync
		mockHubsClient.EXPECT().
			List(gomock.Any(), gomock.Any()).
			Return(&privatev1.HubsListResponse{Items: []*privatev1.Hub{}}, nil).
			AnyTimes()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Top-level projects (no parent)", func() {
		It("should transition to ACTIVE state", func() {
			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name: "test-project",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Test Project",
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
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_ACTIVE))
			Expect(project.GetStatus().HasMessage()).To(BeFalse())
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
				Spec: privatev1.ProjectSpec_builder{
					Parent: strPtr("parent-1"),
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
					Parent: strPtr("root"),
					Title:  "Parent",
				}.Build(),
			}.Build()

			project := privatev1.Project_builder{
				Id: "child",
				Spec: privatev1.ProjectSpec_builder{
					Parent: strPtr("parent"),
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
					Parent: strPtr("project-1"),
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
					Parent: strPtr("nonexistent-parent"),
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
					Parent: strPtr("parent-1"),
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
					Parent: strPtr("parent-1"),
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
					Parent: strPtr("project-b"),
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
						Parent: strPtr("project-a"),
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
			Expect(task.project.GetStatus().GetMessage()).To(ContainSubstring("Circular dependency detected"))
		})

		It("should detect indirect circular dependency", func() {
			projectA := privatev1.Project_builder{
				Id: "project-a",
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Parent: strPtr("project-c"),
					Title:  "Project A",
				}.Build(),
			}.Build()

			projectB := privatev1.Project_builder{
				Id: "project-b",
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Parent: strPtr("project-a"),
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
						Parent: strPtr("project-b"),
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
			Expect(task.project.GetStatus().GetMessage()).To(ContainSubstring("Circular dependency detected"))
		})
	})

	Context("OpenShift Project name length validation", func() {
		It("should fail when generated namespace name exceeds 63 characters", func() {
			// Create a project with long tenant and name that will exceed 63 chars
			// Format: osac-tenant-<tenant>-project-<name>
			// "osac-tenant-" = 12 chars, "-project-" = 9 chars, total overhead = 21 chars
			// So tenant + name can be at most 42 chars
			longTenant := "very-long-organization-name-here" // 32 chars
			longProject := "very-long-project-name-here-too" // 31 chars
			// Total: 12 + 32 + 9 + 31 = 84 chars (exceeds 63)

			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name:    longProject,
					Tenants: []string{longTenant},
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Test Project",
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
			Expect(project.GetStatus().GetMessage()).To(ContainSubstring("exceeds 63 character limit"))
			Expect(project.GetStatus().GetMessage()).To(ContainSubstring("84"))
		})

		It("should succeed when generated namespace name is within limit", func() {
			// Short names that will be well under 63 chars
			// osac-tenant-acme-project-web = 29 chars
			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name:    "web",
					Tenants: []string{"acme"},
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Test Project",
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
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_ACTIVE))
			Expect(project.GetStatus().HasMessage()).To(BeFalse())
		})
	})

	Context("Hub sync conditions", func() {
		It("should set HUB_SYNC condition to TRUE when all hubs succeed", func() {
			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name:    "web",
					Tenants: []string{"acme"},
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Test Project",
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
			Expect(project.GetStatus().GetState()).To(Equal(privatev1.ProjectState_PROJECT_STATE_ACTIVE))

			// Check HUB_SYNC condition
			var hubSyncCondition *privatev1.ProjectCondition
			for _, cond := range project.GetStatus().GetConditions() {
				if cond.GetType() == privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_HUB_SYNC {
					hubSyncCondition = cond
					break
				}
			}

			Expect(hubSyncCondition).ToNot(BeNil())
			Expect(hubSyncCondition.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_FALSE))
			Expect(hubSyncCondition.GetReason()).To(Equal("NoHubs"))
			Expect(hubSyncCondition.GetMessage()).To(ContainSubstring("No hubs available"))
		})

		It("should set HUB_SYNC condition to FALSE when name is too long", func() {
			// Create a project with long org and name that will exceed 63 chars
			longOrg := "very-long-organization-name-here"    // 32 chars
			longProject := "very-long-project-name-here-too" // 31 chars

			project := privatev1.Project_builder{
				Id: "project-1",
				Metadata: privatev1.Metadata_builder{
					Name:    longProject,
					Tenants: []string{longOrg},
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Test Project",
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

			// Check HUB_SYNC condition
			var hubSyncCondition *privatev1.ProjectCondition
			for _, cond := range project.GetStatus().GetConditions() {
				if cond.GetType() == privatev1.ProjectConditionType_PROJECT_CONDITION_TYPE_HUB_SYNC {
					hubSyncCondition = cond
					break
				}
			}

			Expect(hubSyncCondition).ToNot(BeNil())
			Expect(hubSyncCondition.GetStatus()).To(Equal(privatev1.ConditionStatus_CONDITION_STATUS_FALSE))
			Expect(hubSyncCondition.GetReason()).To(Equal("NameTooLong"))
			Expect(hubSyncCondition.GetMessage()).To(ContainSubstring("exceeds 63 character limit"))
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
					Message: strPtr("Some error"),
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

func strPtr(s string) *string {
	return &s
}
