/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package it

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("Projects", func() {
	var (
		ctx    context.Context
		client publicv1.ProjectsClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = publicv1.NewProjectsClient(tool.ExternalView().UserConn())
	})

	It("Can create and get a project", func() {
		name := fmt.Sprintf("test-%s", uuid.New())
		createResponse, err := client.Create(ctx, publicv1.ProjectsCreateRequest_builder{
			Object: publicv1.Project_builder{
				Metadata: publicv1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: publicv1.ProjectSpec_builder{
					Title: "My Test Project",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()
		Expect(object.GetId()).ToNot(BeEmpty())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, publicv1.ProjectsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
		})

		getResponse, err := client.Get(ctx, publicv1.ProjectsGetRequest_builder{
			Id: object.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		fetched := getResponse.GetObject()
		Expect(fetched.GetMetadata().GetName()).To(Equal(name))
		Expect(fetched.GetSpec().GetTitle()).To(Equal("My Test Project"))
		Expect(fetched.GetMetadata().HasCreationTimestamp()).To(BeTrue())
		Expect(fetched.GetMetadata().HasDeletionTimestamp()).To(BeFalse())
	})

	It("Can list projects", func() {
		name := fmt.Sprintf("test-%s", uuid.New())
		createResponse, err := client.Create(ctx, publicv1.ProjectsCreateRequest_builder{
			Object: publicv1.Project_builder{
				Metadata: publicv1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: publicv1.ProjectSpec_builder{
					Title: "List Test",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, publicv1.ProjectsDeleteRequest_builder{
				Id: id,
			}.Build())
		})

		listResponse, err := client.List(ctx, publicv1.ProjectsListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		ids := make([]string, len(listResponse.GetItems()))
		for i, item := range listResponse.GetItems() {
			ids[i] = item.GetId()
		}
		Expect(ids).To(ContainElement(id))
	})

	It("Can update a project title", func() {
		name := fmt.Sprintf("test-%s", uuid.New())
		createResponse, err := client.Create(ctx, publicv1.ProjectsCreateRequest_builder{
			Object: publicv1.Project_builder{
				Metadata: publicv1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: publicv1.ProjectSpec_builder{
					Title: "Original Title",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, publicv1.ProjectsDeleteRequest_builder{
				Id: id,
			}.Build())
		})

		updateResponse, err := client.Update(ctx, publicv1.ProjectsUpdateRequest_builder{
			Object: publicv1.Project_builder{
				Id: id,
				Spec: publicv1.ProjectSpec_builder{
					Title: "Updated Title",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResponse.GetObject().GetSpec().GetTitle()).To(Equal("Updated Title"))

		getResponse, err := client.Get(ctx, publicv1.ProjectsGetRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResponse.GetObject().GetSpec().GetTitle()).To(Equal("Updated Title"))
	})

	It("Can delete a project", func() {
		name := fmt.Sprintf("test-%s", uuid.New())
		createResponse, err := client.Create(ctx, publicv1.ProjectsCreateRequest_builder{
			Object: publicv1.Project_builder{
				Metadata: publicv1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: publicv1.ProjectSpec_builder{
					Title: "Delete Test",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()

		_, err = client.Delete(ctx, publicv1.ProjectsDeleteRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		Eventually(
			func(g Gomega) {
				_, err := client.Get(ctx, publicv1.ProjectsGetRequest_builder{
					Id: id,
				}.Build())
				g.Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				g.Expect(ok).To(BeTrue())
				g.Expect(status.Code()).To(Equal(grpccodes.NotFound))
			},
			time.Minute,
			time.Second,
		).Should(Succeed())
	})

	It("Returns not found when getting non-existent project", func() {
		_, err := client.Get(ctx, publicv1.ProjectsGetRequest_builder{
			Id: "does-not-exist",
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})

	It("Returns not found when updating non-existent project", func() {
		_, err := client.Update(ctx, publicv1.ProjectsUpdateRequest_builder{
			Object: publicv1.Project_builder{
				Id: "does-not-exist",
				Spec: publicv1.ProjectSpec_builder{
					Title: "Should Fail",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})

	It("Returns not found when deleting non-existent project", func() {
		_, err := client.Delete(ctx, publicv1.ProjectsDeleteRequest_builder{
			Id: "does-not-exist",
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})

	It("Preserves labels and annotations", func() {
		name := fmt.Sprintf("test-%s", uuid.New())
		createResponse, err := client.Create(ctx, publicv1.ProjectsCreateRequest_builder{
			Object: publicv1.Project_builder{
				Metadata: publicv1.Metadata_builder{
					Name: name,
					Labels: map[string]string{
						"env":  "test",
						"team": "platform",
					},
					Annotations: map[string]string{
						"note": "integration-test",
					},
				}.Build(),
				Spec: publicv1.ProjectSpec_builder{
					Title: "Labels Test",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, publicv1.ProjectsDeleteRequest_builder{
				Id: id,
			}.Build())
		})

		getResponse, err := client.Get(ctx, publicv1.ProjectsGetRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		metadata := getResponse.GetObject().GetMetadata()
		Expect(metadata.GetLabels()).To(HaveKeyWithValue("env", "test"))
		Expect(metadata.GetLabels()).To(HaveKeyWithValue("team", "platform"))
		Expect(metadata.GetAnnotations()).To(HaveKeyWithValue("note", "integration-test"))
	})
})

var _ = Describe("Project hierarchy", func() {
	var (
		ctx    context.Context
		client privatev1.ProjectsClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewProjectsClient(tool.InternalView().AdminConn())
	})

	It("Derives parent project from hierarchical name", func() {
		parentName := fmt.Sprintf("test-%s", uuid.New())
		childName := fmt.Sprintf("%s.child", parentName)

		By(fmt.Sprintf("Creating parent project %q", parentName))
		parentResponse, err := client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   parentName,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Parent",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			deleteProject(ctx, client, parentResponse.GetObject().GetId())
		})

		By(fmt.Sprintf("Creating child project %q", childName))
		childResponse, err := client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   childName,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Child",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			deleteProject(ctx, client, childResponse.GetObject().GetId())
		})

		By("Verifying parent is derived from name")
		Expect(childResponse.GetObject().GetMetadata().GetProject()).To(Equal(parentName))
	})

	It("Derives parent for three-level hierarchy", func() {
		level1 := fmt.Sprintf("test-%s", uuid.New())
		level2 := fmt.Sprintf("%s.team", level1)
		level3 := fmt.Sprintf("%s.sub", level2)

		By("Creating level-1 project")
		resp1, err := client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   level1,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Level 1",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			deleteProject(ctx, client, resp1.GetObject().GetId())
		})

		By("Creating level-2 project")
		resp2, err := client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   level2,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Level 2",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			deleteProject(ctx, client, resp2.GetObject().GetId())
		})

		By("Creating level-3 project")
		resp3, err := client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   level3,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Level 3",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			deleteProject(ctx, client, resp3.GetObject().GetId())
		})

		By("Verifying parent derivation")
		Expect(resp3.GetObject().GetMetadata().GetProject()).To(Equal(level2))
	})

	It("Rejects project name with more than 4 segments", func() {
		name := fmt.Sprintf("a.b.c.d.%s", uuid.New())
		_, err := client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   name,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Too Deep",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
	})

	It("Rejects mismatched explicit parent project", func() {
		parentName := fmt.Sprintf("test-%s", uuid.New())

		By("Creating parent project")
		parentResponse, err := client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   parentName,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Parent",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			deleteProject(ctx, client, parentResponse.GetObject().GetId())
		})

		By("Creating child with wrong explicit parent")
		childName := fmt.Sprintf("%s.child", parentName)
		_, err = client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    childName,
					Tenant:  "users",
					Project: "wrong-parent",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Child",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
	})
})

var _ = Describe("Project edge cases", func() {
	var (
		client privatev1.ProjectsClient
	)

	BeforeEach(func() {
		client = privatev1.NewProjectsClient(tool.InternalView().AdminConn())
	})

	It("Rejects duplicate project name within same tenant", func(ctx context.Context) {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating project %q", name))
		resp, err := client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   name,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "First",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			deleteProject(ctx, client, resp.GetObject().GetId())
		})

		By("Attempting to create project with same name")
		_, err = client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   name,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Duplicate",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.AlreadyExists))
	})

	It("Can re-create project after delete with same name", func(ctx context.Context) {
		name := fmt.Sprintf("test-%s", uuid.New())

		By("Creating project")
		resp, err := client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   name,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Original",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := resp.GetObject().GetId()

		By("Deleting project")
		deleteProject(ctx, client, id)

		By("Re-creating project with same name")
		resp2, err := client.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   name,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Re-created",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			deleteProject(ctx, client, resp2.GetObject().GetId())
		})

		Expect(resp2.GetObject().GetId()).ToNot(Equal(id))
		Expect(resp2.GetObject().GetSpec().GetTitle()).To(Equal("Re-created"))
	})
})
