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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

var _ = Describe("Private tenants server", func() {
	var privateServer *PrivateOrganizationsServer

	BeforeEach(func() {
		var err error

		// Create server (without notifier for testing):
		privateServer, err = NewPrivateOrganizationsServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Creates a tenant", func() {
		// Create request:
		request := privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-tenant",
				}.Build(),
			}.Build(),
		}.Build()

		// Create tenant:
		response, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object).ToNot(BeNil())
		Expect(response.Object.Id).ToNot(BeEmpty())
		Expect(response.Object.Metadata.Name).To(Equal("my-tenant"))
	})

	It("Lists tenants", func() {
		// Create a tenant first:
		createReq := privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-tenant",
				}.Build(),
			}.Build(),
		}.Build()
		_, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// List tenants:
		listResp, err := privateServer.List(ctx, &privatev1.OrganizationsListRequest{
			Filter: new("this.metadata.name == 'my-tenant'"),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Size).To(Equal(int32(1)))
		Expect(listResp.Items).To(HaveLen(1))
		Expect(listResp.Items[0].Metadata.Name).To(Equal("my-tenant"))
	})

	It("Gets a tenant by ID", func() {
		// Create a tenant:
		createReq := privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-tenant",
				}.Build(),
			}.Build(),
		}.Build()
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Get the tenant:
		getResp, err := privateServer.Get(ctx, privatev1.OrganizationsGetRequest_builder{
			Id: createResp.Object.Id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
		Expect(getResp.Object.Metadata.Name).To(Equal("my-tenant"))
	})

	It("Deletes a tenant", func() {
		// Create a tenant:
		createReq := privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-tenant",
				}.Build(),
			}.Build(),
		}.Build()
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Delete the tenant:
		_, err = privateServer.Delete(ctx, privatev1.OrganizationsDeleteRequest_builder{
			Id: createResp.Object.Id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
	})

	It("Updates a tenant", func() {
		// Create a tenant:
		createReq := privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-tenant",
				}.Build(),
			}.Build(),
		}.Build()
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Update the tenant:
		updateReq := privatev1.OrganizationsUpdateRequest_builder{
			Object: privatev1.Organization_builder{
				Id: createResp.Object.Id,
				Status: privatev1.OrganizationStatus_builder{
					State: privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED,
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
		Expect(updateResp.Object.Status.State).To(Equal(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED))
	})

	It("Rejects creation of a tenant with an empty name", func() {
		response, err := privateServer.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		Expect(response).To(BeNil())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(Equal(
			"field 'metadata.name' is mandatory",
		))
	})

	It("Rejects creation of a tenant with an identifier different from the name", func() {
		response, err := privateServer.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Id: "your-tenant",
				Metadata: privatev1.Metadata_builder{
					Name: "my-tenant",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		Expect(response).To(BeNil())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(Equal(
			"field 'id' must be empty or equal to field 'metadata.name'",
		))
	})

	It("Uses the name as the identifier if no identifier is provided", func() {
		response, err := privateServer.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-tenant",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.GetObject().GetId()).To(Equal("my-tenant"))
	})

	It("Rejects an explicit tenant different than the name", func() {
		response, err := privateServer.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name:   "my-tenant",
					Tenant: "your-tenant",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		Expect(response).To(BeNil())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(Equal(
			"field 'metadata.tenant' must be empty or equal to field 'metadata.name'",
		))
	})

	It("Uses the name as the tenant if no tenant is provided", func() {
		response, err := privateServer.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-tenant",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.GetObject().GetMetadata().GetTenant()).To(Equal("my-tenant"))
	})

	It("Rejects update of the name of a tenant", func() {
		createResponse, err := privateServer.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-tenant",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()
		id := object.GetId()
		updateResponse, err := privateServer.Update(ctx, privatev1.OrganizationsUpdateRequest_builder{
			Object: privatev1.Organization_builder{
				Id: id,
				Metadata: privatev1.Metadata_builder{
					Name: "your-name",
				}.Build(),
			}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{
					"metadata.name",
				},
			},
		}.Build())
		Expect(err).To(HaveOccurred())
		Expect(updateResponse).To(BeNil())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(Equal(
			"field 'metadata.name' is immutable",
		))
	})

	It("Rejects update of the tenant of a tenant", func() {
		createResponse, err := privateServer.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "my-tenant",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()
		id := object.GetId()
		updateResponse, err := privateServer.Update(ctx, privatev1.OrganizationsUpdateRequest_builder{
			Object: privatev1.Organization_builder{
				Id: id,
				Metadata: privatev1.Metadata_builder{
					Tenant: "your-tenant",
				}.Build(),
			}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{
				Paths: []string{
					"metadata.tenant",
				},
			},
		}.Build())
		Expect(err).To(HaveOccurred())
		Expect(updateResponse).To(BeNil())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(Equal(
			"field 'metadata.tenant' is immutable",
		))
	})

	Describe("Domain validation", func() {
		DescribeTable(
			"Accepts valid domains",
			func(domains []string) {
				err := privateServer.validateDomains(domains)
				Expect(err).ToNot(HaveOccurred())
			},
			Entry(
				"Empty list",
				[]string{},
			),
			Entry(
				"Simple domain",
				[]string{
					"example.com",
				},
			),
			Entry(
				"Subdomain",
				[]string{
					"sub.example.org",
				},
			),
			Entry(
				"Multiple domains",
				[]string{
					"example.com",
					"example.org",
				},
			),
		)

		DescribeTable(
			"Rejects invalid domains",
			func(domains []string, msgSubstring string) {
				err := privateServer.validateDomains(domains)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(status.Message()).To(ContainSubstring(msgSubstring))
			},
			Entry(
				"Empty domain string",
				[]string{
					"",
				},
				"must not be empty",
			),
			Entry(
				"Longer than 253 characters",
				[]string{
					strings.Repeat("a", 250) + ".com",
				},
				"must be at most 253 characters",
			),
			Entry(
				"IPv4 address",
				[]string{
					"192.168.1.1",
				},
				"must be a DNS hostname, not an IP address",
			),
			Entry(
				"IPv6 address",
				[]string{
					"::1",
				},
				"must be a DNS hostname, not an IP address",
			),
			Entry(
				"fewer than two labels",
				[]string{
					"localhost",
				},
				"must have at least two labels",
			),
			Entry(
				"uppercase letters",
				[]string{
					"Example.com",
				},
				"contains invalid label",
			),
			Entry(
				"underscores",
				[]string{
					"my_domain.com",
				},
				"contains invalid label",
			),
			Entry(
				"label starting with hyphen",
				[]string{
					"-example.com",
				},
				"contains invalid label",
			),
			Entry(
				"label ending with hyphen",
				[]string{
					"example-.com",
				},
				"contains invalid label",
			),
			Entry(
				"label longer than 63 characters",
				[]string{
					strings.Repeat("a", 64) + ".com",
				},
				"must be at most 63 characters",
			),
			Entry(
				"duplicate domains",
				[]string{
					"example.com",
					"example.com",
				},
				"contains duplicate domain",
			),
		)
	})
})
