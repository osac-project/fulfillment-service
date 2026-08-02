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

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("Project memberships", func() {
	var (
		ctx               context.Context
		client            publicv1.ProjectMembershipsClient
		projectsClient    privatev1.ProjectsClient
		projectId         string
		projectName       string
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = publicv1.NewProjectMembershipsClient(tool.ExternalView().UserConn())
		projectsClient = privatev1.NewProjectsClient(tool.InternalView().AdminConn())

		projectName = fmt.Sprintf("test-%s", uuid.New())
		resp, err := projectsClient.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   projectName,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Membership Test Project",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		projectId = resp.GetObject().GetId()
		DeferCleanup(func() {
			deleteProject(ctx, projectsClient, projectId)
		})
	})

	It("Can create and get a membership with VIEWER role", func() {
		createResponse, err := client.Create(ctx, publicv1.ProjectMembershipsCreateRequest_builder{
			Object: publicv1.ProjectMembership_builder{
				Metadata: publicv1.Metadata_builder{
					Name:    fmt.Sprintf("test-%s", uuid.New()),
					Project: projectName,
				}.Build(),
				Spec: publicv1.ProjectMembershipSpec_builder{
					Role:  publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER,
					Users: []string{"user-1"},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()
		Expect(object.GetId()).ToNot(BeEmpty())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, publicv1.ProjectMembershipsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
		})

		getResponse, err := client.Get(ctx, publicv1.ProjectMembershipsGetRequest_builder{
			Id: object.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		fetched := getResponse.GetObject()
		Expect(fetched.GetSpec().GetRole()).To(Equal(
			publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER,
		))
		Expect(fetched.GetSpec().GetUsers()).To(ConsistOf("user-1"))
		Expect(fetched.GetMetadata().GetProject()).To(Equal(projectName))
		Expect(fetched.GetMetadata().HasCreationTimestamp()).To(BeTrue())
	})

	It("Can create a membership with MANAGER role", func() {
		createResponse, err := client.Create(ctx, publicv1.ProjectMembershipsCreateRequest_builder{
			Object: publicv1.ProjectMembership_builder{
				Metadata: publicv1.Metadata_builder{
					Name:    fmt.Sprintf("test-%s", uuid.New()),
					Project: projectName,
				}.Build(),
				Spec: publicv1.ProjectMembershipSpec_builder{
					Role:  publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
					Users: []string{"user-2"},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, publicv1.ProjectMembershipsDeleteRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
		})

		Expect(createResponse.GetObject().GetSpec().GetRole()).To(Equal(
			publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
		))
	})

	It("Can list memberships", func() {
		createResponse, err := client.Create(ctx, publicv1.ProjectMembershipsCreateRequest_builder{
			Object: publicv1.ProjectMembership_builder{
				Metadata: publicv1.Metadata_builder{
					Name:    fmt.Sprintf("test-%s", uuid.New()),
					Project: projectName,
				}.Build(),
				Spec: publicv1.ProjectMembershipSpec_builder{
					Role:  publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER,
					Users: []string{"user-3"},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, publicv1.ProjectMembershipsDeleteRequest_builder{
				Id: id,
			}.Build())
		})

		listResponse, err := client.List(ctx, publicv1.ProjectMembershipsListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		ids := make([]string, len(listResponse.GetItems()))
		for i, item := range listResponse.GetItems() {
			ids[i] = item.GetId()
		}
		Expect(ids).To(ContainElement(id))
	})

	It("Can update membership role", func() {
		createResponse, err := client.Create(ctx, publicv1.ProjectMembershipsCreateRequest_builder{
			Object: publicv1.ProjectMembership_builder{
				Metadata: publicv1.Metadata_builder{
					Name:    fmt.Sprintf("test-%s", uuid.New()),
					Project: projectName,
				}.Build(),
				Spec: publicv1.ProjectMembershipSpec_builder{
					Role:  publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER,
					Users: []string{"user-4"},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, publicv1.ProjectMembershipsDeleteRequest_builder{
				Id: id,
			}.Build())
		})

		updateResponse, err := client.Update(ctx, publicv1.ProjectMembershipsUpdateRequest_builder{
			Object: publicv1.ProjectMembership_builder{
				Id: id,
				Spec: publicv1.ProjectMembershipSpec_builder{
					Role:  publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
					Users: []string{"user-4"},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResponse.GetObject().GetSpec().GetRole()).To(Equal(
			publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
		))

		getResponse, err := client.Get(ctx, publicv1.ProjectMembershipsGetRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResponse.GetObject().GetSpec().GetRole()).To(Equal(
			publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
		))
	})

	It("Can delete a membership", func() {
		createResponse, err := client.Create(ctx, publicv1.ProjectMembershipsCreateRequest_builder{
			Object: publicv1.ProjectMembership_builder{
				Metadata: publicv1.Metadata_builder{
					Name:    fmt.Sprintf("test-%s", uuid.New()),
					Project: projectName,
				}.Build(),
				Spec: publicv1.ProjectMembershipSpec_builder{
					Role:  publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER,
					Users: []string{"user-5"},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()

		_, err = client.Delete(ctx, publicv1.ProjectMembershipsDeleteRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		_, err = client.Get(ctx, publicv1.ProjectMembershipsGetRequest_builder{
			Id: id,
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})

	It("Returns not found when getting non-existent membership", func() {
		_, err := client.Get(ctx, publicv1.ProjectMembershipsGetRequest_builder{
			Id: "does-not-exist",
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})

	It("Returns not found when updating non-existent membership", func() {
		_, err := client.Update(ctx, publicv1.ProjectMembershipsUpdateRequest_builder{
			Object: publicv1.ProjectMembership_builder{
				Id: "does-not-exist",
				Spec: publicv1.ProjectMembershipSpec_builder{
					Role:  publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER,
					Users: []string{"user-x"},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})

	It("Returns not found when deleting non-existent membership", func() {
		_, err := client.Delete(ctx, publicv1.ProjectMembershipsDeleteRequest_builder{
			Id: "does-not-exist",
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})

	It("Can create membership with multiple users", func() {
		createResponse, err := client.Create(ctx, publicv1.ProjectMembershipsCreateRequest_builder{
			Object: publicv1.ProjectMembership_builder{
				Metadata: publicv1.Metadata_builder{
					Name:    fmt.Sprintf("test-%s", uuid.New()),
					Project: projectName,
				}.Build(),
				Spec: publicv1.ProjectMembershipSpec_builder{
					Role:  publicv1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER,
					Users: []string{"user-a", "user-b", "user-c"},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, publicv1.ProjectMembershipsDeleteRequest_builder{
				Id: id,
			}.Build())
		})

		getResponse, err := client.Get(ctx, publicv1.ProjectMembershipsGetRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResponse.GetObject().GetSpec().GetUsers()).To(ConsistOf("user-a", "user-b", "user-c"))
	})
})

var _ = Describe("Project membership filtering", func() {
	var (
		ctx               context.Context
		client            privatev1.ProjectMembershipsClient
		projectsClient    privatev1.ProjectsClient
		projectNameA      string
		projectNameB      string
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewProjectMembershipsClient(tool.InternalView().AdminConn())
		projectsClient = privatev1.NewProjectsClient(tool.InternalView().AdminConn())

		projectNameA = fmt.Sprintf("test-%s", uuid.New())
		respA, err := projectsClient.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   projectNameA,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Project A",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			deleteProject(ctx, projectsClient, respA.GetObject().GetId())
		})

		projectNameB = fmt.Sprintf("test-%s", uuid.New())
		respB, err := projectsClient.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   projectNameB,
					Tenant: "users",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Project B",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			deleteProject(ctx, projectsClient, respB.GetObject().GetId())
		})
	})

	It("Can filter memberships by project", func() {
		By("Creating membership in project A")
		respA, err := client.Create(ctx, privatev1.ProjectMembershipsCreateRequest_builder{
			Object: privatev1.ProjectMembership_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    fmt.Sprintf("test-%s", uuid.New()),
					Tenant:  "users",
					Project: projectNameA,
				}.Build(),
				Spec: privatev1.ProjectMembershipSpec_builder{
					Role:  privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_VIEWER,
					Users: []string{"filter-user-1"},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		idA := respA.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.ProjectMembershipsDeleteRequest_builder{
				Id: idA,
			}.Build())
		})

		By("Creating membership in project B")
		respB, err := client.Create(ctx, privatev1.ProjectMembershipsCreateRequest_builder{
			Object: privatev1.ProjectMembership_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    fmt.Sprintf("test-%s", uuid.New()),
					Tenant:  "users",
					Project: projectNameB,
				}.Build(),
				Spec: privatev1.ProjectMembershipSpec_builder{
					Role:  privatev1.ProjectMembershipRole_PROJECT_MEMBERSHIP_ROLE_MANAGER,
					Users: []string{"filter-user-2"},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		idB := respB.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.ProjectMembershipsDeleteRequest_builder{
				Id: idB,
			}.Build())
		})

		By("Listing memberships filtered by project A")
		filter := fmt.Sprintf("this.metadata.project == %q", projectNameA)
		listResponse, err := client.List(ctx, privatev1.ProjectMembershipsListRequest_builder{
			Filter: &filter,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		ids := make([]string, len(listResponse.GetItems()))
		for i, item := range listResponse.GetItems() {
			ids[i] = item.GetId()
		}
		Expect(ids).To(ContainElement(idA))
		Expect(ids).ToNot(ContainElement(idB))
	})
})
