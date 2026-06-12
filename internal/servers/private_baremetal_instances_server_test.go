/*
Copyright (c) 2025 Red Hat Inc.

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

// A real ed25519 public key in OpenSSH authorized_keys format for testing.
const testSSHPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG8K1ZuSC7tmzxD5LJJXwkCfStVEjzXWYCFhJaLBxWAn test@example.com"

var _ = Describe("Private bare metal instances server", func() {
	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivateBareMetalInstancesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivateBareMetalInstancesServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPrivateBareMetalInstancesServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivateBareMetalInstancesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var (
			server        *PrivateBareMetalInstancesServer
			catalogServer *PrivateBareMetalInstanceCatalogItemsServer
			catalogItemID string
		)

		BeforeEach(func() {
			var err error

			catalogServer, err = NewPrivateBareMetalInstanceCatalogItemsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			server, err = NewPrivateBareMetalInstancesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create a published catalog item for use in tests.
			catalogResp, err := catalogServer.Create(ctx, privatev1.BareMetalInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.BareMetalInstanceCatalogItem_builder{
					Title:     "Test catalog item",
					Template:  "test-template",
					Published: true,
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			catalogItemID = catalogResp.GetObject().GetId()
		})

		It("Creates object with minimal spec", func() {
			response, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetObject().GetId()).ToNot(BeEmpty())
			Expect(response.GetObject().GetSpec().GetCatalogItem()).To(Equal(catalogItemID))
		})

		It("Creates object with valid SSH key", func() {
			response, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
						SshKey:      new(testSSHPublicKey),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetObject().GetSpec().GetSshKey()).To(Equal(testSSHPublicKey))
		})

		It("Rejects nonexistent catalog item", func() {
			_, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: "does-not-exist",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.NotFound))
			Expect(status.Message()).To(ContainSubstring("does-not-exist"))
		})

		It("Rejects unpublished catalog item", func() {
			unpubResp, err := catalogServer.Create(ctx, privatev1.BareMetalInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.BareMetalInstanceCatalogItem_builder{
					Title:     "Unpublished item",
					Template:  "test-template",
					Published: false,
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			unpubID := unpubResp.GetObject().GetId()

			_, err = server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: unpubID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.NotFound))
			Expect(status.Message()).To(ContainSubstring("not published"))
		})

		// validateSpec runs before catalog item lookup, so invalid SSH key/user data
		// fail with InvalidArgument before the catalog item is checked.
		It("Rejects invalid SSH key at create time", func() {
			_, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
						SshKey:      new("not-a-valid-ssh-key"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(ContainSubstring("spec.ssh_key"))
		})

		It("Rejects user data exceeding 64 KB at create time", func() {
			bigData := strings.Repeat("x", bareMetalInstanceUserDataMaxBytes+1)
			_, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
						UserData:    new(bigData),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(ContainSubstring("spec.user_data"))
			Expect(status.Message()).To(ContainSubstring("exceeds the maximum"))
		})

		It("Accepts user data at exactly 64 KB", func() {
			exactData := strings.Repeat("x", bareMetalInstanceUserDataMaxBytes)
			_, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
						UserData:    new(exactData),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Rejects PATCH that changes catalog_item", func() {
			createResponse, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			secondResp, err := catalogServer.Create(ctx, privatev1.BareMetalInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.BareMetalInstanceCatalogItem_builder{
					Title:     "Second catalog item",
					Template:  "test-template",
					Published: true,
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			secondID := secondResp.GetObject().GetId()

			_, err = server.Update(ctx, privatev1.BareMetalInstancesUpdateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Id: object.GetId(),
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: secondID,
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.catalog_item"},
				},
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(ContainSubstring("catalog_item is immutable"))
		})

		It("Rejects PATCH that changes ssh_key", func() {
			createResponse, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
						SshKey:      new(testSSHPublicKey),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			otherKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBe5EVW4cHjAFNa8jMJQqLGBJENvJRfH+Q2lOjFr93vd other@example.com"
			_, err = server.Update(ctx, privatev1.BareMetalInstancesUpdateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Id: object.GetId(),
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
						SshKey:      new(otherKey),
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.ssh_key"},
				},
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(ContainSubstring("ssh_key is immutable"))
		})

		It("Rejects PATCH that changes user_data", func() {
			userData := "original user data"
			createResponse, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
						UserData:    new(userData),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			newData := "changed user data"
			_, err = server.Update(ctx, privatev1.BareMetalInstancesUpdateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Id: object.GetId(),
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
						UserData:    new(newData),
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.user_data"},
				},
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(ContainSubstring("user_data is immutable"))
		})

		It("Allows PATCH that does not touch immutable fields", func() {
			createResponse, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
						RunStrategy: new(privatev1.BareMetalInstanceRunStrategy_BARE_METAL_INSTANCE_RUN_STRATEGY_ALWAYS),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			_, err = server.Update(ctx, privatev1.BareMetalInstancesUpdateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Id: object.GetId(),
					Spec: privatev1.BareMetalInstanceSpec_builder{
						RunStrategy: new(privatev1.BareMetalInstanceRunStrategy_BARE_METAL_INSTANCE_RUN_STRATEGY_HALTED),
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.run_strategy"},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows PATCH with no update mask (full replace) preserving same immutable fields", func() {
			createResponse, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			_, err = server.Update(ctx, privatev1.BareMetalInstancesUpdateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Id: object.GetId(),
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Signals object", func() {
			createResponse, err := server.Create(ctx, privatev1.BareMetalInstancesCreateRequest_builder{
				Object: privatev1.BareMetalInstance_builder{
					Spec: privatev1.BareMetalInstanceSpec_builder{
						CatalogItem: catalogItemID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			_, err = server.Signal(ctx, privatev1.BareMetalInstancesSignalRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
