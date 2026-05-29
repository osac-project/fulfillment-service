/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

var _ = Describe("Private projects server", func() {
	var privateServer *PrivateProjectsServer

	BeforeEach(func() {
		var err error

		createTenant(ctx, "my-tenant")
		createTenant(ctx, "your-tenant")

		// Create server (without notifier for testing):
		privateServer, err = NewPrivateProjectsServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Creates a project", func() {
		// Create request:
		request := privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "my-project",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title:       "My Project",
					Description: new("Test project"),
				}.Build(),
			}.Build(),
		}.Build()

		// Create project:
		response, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object).ToNot(BeNil())
		Expect(response.Object.Id).ToNot(BeEmpty())
		Expect(response.Object.Metadata.Name).To(Equal("my-project"))
		Expect(response.Object.Spec.Title).To(Equal("My Project"))
		Expect(response.Object.Spec.Description).To(HaveValue(Equal("Test project")))
	})

	It("Creates a nested project with parent", func() {
		// Create parent project:
		parentReq := privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "parent-project",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Parent Project",
				}.Build(),
			}.Build(),
		}.Build()
		parentResp, err := privateServer.Create(ctx, parentReq)
		Expect(err).ToNot(HaveOccurred())

		// Create child project:
		childReq := privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "child-project",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title:  "Child Project",
					Parent: new(parentResp.Object.Id),
				}.Build(),
			}.Build(),
		}.Build()
		childResp, err := privateServer.Create(ctx, childReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(childResp.Object.Spec.Parent).To(HaveValue(Equal(parentResp.Object.Id)))
	})

	It("Lists projects", func() {
		// Create a project first:
		createReq := privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "my-project",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "My Project",
				}.Build(),
			}.Build(),
		}.Build()
		_, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// List projects:
		listResp, err := privateServer.List(ctx, &privatev1.ProjectsListRequest{
			Filter: new("this.metadata.name == 'my-project'"),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Size).To(Equal(int32(1)))
		Expect(listResp.Items).To(HaveLen(1))
		Expect(listResp.Items[0].Metadata.Name).To(Equal("my-project"))
	})

	It("Lists projects by tenant", func() {
		// Create projects in different tenants:
		for _, tenant := range []string{"my-tenant", "your-tenant"} {
			createReq := privatev1.ProjectsCreateRequest_builder{
				Object: privatev1.Project_builder{
					Metadata: privatev1.Metadata_builder{
						Name:   "project-" + tenant,
						Tenant: tenant,
					}.Build(),
					Spec: privatev1.ProjectSpec_builder{
						Title: "Project " + tenant,
					}.Build(),
				}.Build(),
			}.Build()
			_, err := privateServer.Create(ctx, createReq)
			Expect(err).ToNot(HaveOccurred())
		}

		// List projects for one of the tenants:
		listResp, err := privateServer.List(ctx, &privatev1.ProjectsListRequest{
			Filter: new("this.metadata.tenant == 'my-tenant'"),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Size).To(Equal(int32(1)))
		Expect(listResp.Items[0].Metadata.Tenant).To(Equal("my-tenant"))
	})

	It("Lists top-level projects (no parent)", func() {
		// Create parent and child:
		parentReq := privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "parent",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Parent",
				}.Build(),
			}.Build(),
		}.Build()
		parentResp, err := privateServer.Create(ctx, parentReq)
		Expect(err).ToNot(HaveOccurred())

		childReq := privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "child",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title:  "Child",
					Parent: new(parentResp.Object.Id),
				}.Build(),
			}.Build(),
		}.Build()
		_, err = privateServer.Create(ctx, childReq)
		Expect(err).ToNot(HaveOccurred())

		// List only top-level projects:
		listResp, err := privateServer.List(ctx, &privatev1.ProjectsListRequest{
			Filter: new("this.metadata.tenant == 'my-tenant' && !has(this.spec.parent)"),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Size).To(Equal(int32(1)))
		Expect(listResp.Items[0].Metadata.Name).To(Equal("parent"))
	})

	It("Gets a project by ID", func() {
		// Create a project:
		createReq := privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "my-project",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "My Project",
				}.Build(),
			}.Build(),
		}.Build()
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Get the project:
		getResp, err := privateServer.Get(ctx, privatev1.ProjectsGetRequest_builder{
			Id: createResp.Object.Id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
		Expect(getResp.Object.Metadata.Name).To(Equal("my-project"))
	})

	It("Deletes a project", func() {
		// Create a project:
		createReq := privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "my-project",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "My Project",
				}.Build(),
			}.Build(),
		}.Build()
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Delete the project:
		_, err = privateServer.Delete(ctx, privatev1.ProjectsDeleteRequest_builder{
			Id: createResp.Object.Id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
	})

	It("Updates a project", func() {
		// Create a project:
		createReq := privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "my-project",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "My Project",
				}.Build(),
			}.Build(),
		}.Build()
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Update the project status:
		updateReq := privatev1.ProjectsUpdateRequest_builder{
			Object: privatev1.Project_builder{
				Id: createResp.Object.Id,
				Status: privatev1.ProjectStatus_builder{
					State: privatev1.ProjectState_PROJECT_STATE_ACTIVE,
				}.Build(),
			}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{
					"status.state",
				},
			},
		}.Build()
		updateResp, err := privateServer.Update(ctx, updateReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResp.Object.Status.State).To(Equal(privatev1.ProjectState_PROJECT_STATE_ACTIVE))
	})

	It("Updates project description", func() {
		// Create a project:
		createReq := privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "my-project",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "My Project",
				}.Build(),
			}.Build(),
		}.Build()
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Update description:
		updateReq := privatev1.ProjectsUpdateRequest_builder{
			Object: privatev1.Project_builder{
				Id: createResp.Object.Id,
				Spec: privatev1.ProjectSpec_builder{
					Description: new("Updated description"),
				}.Build(),
			}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{
					"spec.description",
				},
			},
		}.Build()
		updateResp, err := privateServer.Update(ctx, updateReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResp.Object.Spec.Description).To(HaveValue(Equal("Updated description")))
	})
})
