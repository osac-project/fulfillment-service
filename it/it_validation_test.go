/*
Copyright (c) 2025 Red Hat Inc.

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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

var _ = Describe("Protovalidate validation", func() {
	var (
		ctx                 context.Context
		tenantClient        privatev1.TenantsClient
		vnetClient          privatev1.VirtualNetworksClient
		networkClassClient  privatev1.NetworkClassesClient
		projectsClient      privatev1.ProjectsClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		tenantClient = privatev1.NewTenantsClient(tool.InternalView().AdminConn())
		vnetClient = privatev1.NewVirtualNetworksClient(tool.InternalView().AdminConn())
		networkClassClient = privatev1.NewNetworkClassesClient(tool.InternalView().AdminConn())
		projectsClient = privatev1.NewProjectsClient(tool.InternalView().AdminConn())

		// Create test tenant for Project tests
		_, err := tenantClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-tenant",
				}.Build(),
			}.Build(),
		}.Build())
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("Rejects Tenant with invalid metadata name (too long)", func() {
		// Create a Tenant with name > 63 chars:
		invalidName := "this-name-is-way-too-long-and-exceeds-the-sixty-three-character-limit-for-dns-labels"

		_, err := tenantClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: invalidName,
				}.Build(),
			}.Build(),
		}.Build())

		// Verify validation error:
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue(), "error should be a gRPC status error")
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument), "should return InvalidArgument")
		Expect(status.Message()).To(ContainSubstring("validation"))
		Expect(status.Message()).To(ContainSubstring("name"))
	})

	It("Rejects Tenant with invalid metadata name pattern", func() {
		// Create a Tenant with uppercase (invalid for DNS label):
		invalidName := "Invalid-Name-With-Uppercase"

		_, err := tenantClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: invalidName,
				}.Build(),
			}.Build(),
		}.Build())

		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(ContainSubstring("validation"))
	})

	It("Rejects Tenant with label key that is too long", func() {
		// Create a label key > 316 chars:
		longKey := ""
		for i := 0; i < 320; i++ {
			longKey = longKey + "a"
		}

		_, err := tenantClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "valid-name",
					Labels: map[string]string{
						longKey: "value",
					},
				}.Build(),
			}.Build(),
		}.Build())

		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(ContainSubstring("validation"))
	})

	It("Accepts Tenant with valid metadata", func() {
		validName := "valid-tenant-name"

		response, err := tenantClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: validName,
					Labels: map[string]string{
						"key1":             "value1",
						"example.com/key2": "value2",
					},
					Annotations: map[string]string{
						"annotation-key": "annotation-value",
					},
				}.Build(),
			}.Build(),
		}.Build())

		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object.Metadata.Name).To(Equal(validName))

		// Clean up:
		DeferCleanup(func() {
			_, _ = tenantClient.Delete(ctx, privatev1.TenantsDeleteRequest_builder{
				Id: response.Object.Id,
			}.Build())
		})
	})

	It("Accepts VirtualNetwork with empty name (protovalidate allows empty strings)", func() {
		// VirtualNetwork doesn't require a name (unlike Tenant), so we can test
		// that protovalidate's regex pattern allows empty strings

		// First get a NetworkClass
		listResp, err := networkClassClient.List(ctx, privatev1.NetworkClassesListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Items).ToNot(BeEmpty(), "at least one NetworkClass must exist")
		networkClass := listResp.Items[0].Metadata.Name

		response, err := vnetClient.Create(ctx, privatev1.VirtualNetworksCreateRequest_builder{
			Object: privatev1.VirtualNetwork_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "",
				}.Build(),
				Spec: privatev1.VirtualNetworkSpec_builder{
					NetworkClass: networkClass,
					Region:       "us-east-1",
					Ipv4Cidr:     new("10.0.0.0/16"),
				}.Build(),
			}.Build(),
		}.Build())

		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object.Metadata.Name).To(Equal(""))

		// Clean up:
		DeferCleanup(func() {
			_, _ = vnetClient.Delete(ctx, privatev1.VirtualNetworksDeleteRequest_builder{
				Id: response.Object.Id,
			}.Build())
		})
	})

	It("Accepts Project with dot-separated hierarchical name", func() {
		// Projects use field-level CEL validation that allows dot-separated names
		// like "org.team.project" (each segment is a DNS label, connected with dots)

		// Create parent project hierarchy first: org -> org.team-a -> org.team-a.frontend
		orgProject, err := projectsClient.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "org",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Organization",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = projectsClient.Delete(ctx, privatev1.ProjectsDeleteRequest_builder{
				Id: orgProject.Object.Id,
			}.Build())
		})

		teamProject, err := projectsClient.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "org.team-a",
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Team A",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = projectsClient.Delete(ctx, privatev1.ProjectsDeleteRequest_builder{
				Id: teamProject.Object.Id,
			}.Build())
		})

		hierarchicalName := "org.team-a.frontend"
		response, err := projectsClient.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   hierarchicalName,
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Frontend Team",
				}.Build(),
			}.Build(),
		}.Build())

		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object.Metadata.Name).To(Equal(hierarchicalName))
		// Server should derive parent project from the hierarchical name:
		Expect(response.Object.Metadata.Project).To(Equal("org.team-a"))

		// Clean up:
		DeferCleanup(func() {
			_, _ = projectsClient.Delete(ctx, privatev1.ProjectsDeleteRequest_builder{
				Id: response.Object.Id,
			}.Build())
		})
	})

	It("Rejects Project with invalid segment in hierarchical name", func() {
		// Each dot-separated segment must be a valid DNS label
		invalidName := "org.Team-A.frontend" // "Team-A" has uppercase

		_, err := projectsClient.Create(ctx, privatev1.ProjectsCreateRequest_builder{
			Object: privatev1.Project_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   invalidName,
					Tenant: "my-tenant",
				}.Build(),
				Spec: privatev1.ProjectSpec_builder{
					Title: "Invalid Project",
				}.Build(),
			}.Build(),
		}.Build())

		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		// Should be rejected by CEL validation (project_name_segments)
		Expect(status.Message()).To(ContainSubstring("project name must be"))
	})

	It("Rejects Tenant with dots (proves IGNORE_ALWAYS is working for Projects)", func() {
		// Tenants DON'T use IGNORE_ALWAYS, so dots should be rejected by Metadata pattern.
		// This proves that when Projects accept dots, it's because IGNORE_ALWAYS is working.
		nameWithDots := "org.team"

		_, err := tenantClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: nameWithDots,
				}.Build(),
			}.Build(),
		}.Build())

		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		// Should be rejected by Metadata.name pattern (doesn't allow dots)
		Expect(status.Message()).To(ContainSubstring("validation"))
	})
})
