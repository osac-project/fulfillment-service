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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"go.uber.org/mock/gomock"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Private compute instance catalog items server", func() {
	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivateComputeInstanceCatalogItemsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivateComputeInstanceCatalogItemsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPrivateComputeInstanceCatalogItemsServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivateComputeInstanceCatalogItemsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Can be built without explicit reference checker", func() {
			server, err := NewPrivateComputeInstanceCatalogItemsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var server *PrivateComputeInstanceCatalogItemsServer

		BeforeEach(func() {
			var err error

			// Create the server:
			server, err = NewPrivateComputeInstanceCatalogItemsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Creates object", func() {
			response, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Title:       "My CI catalog item",
					Description: "My description.",
					Template:    "my-ci-template-id",
					Published:   true,
					Tenant:      "my-tenant",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())
			Expect(object.GetTitle()).To(Equal("My CI catalog item"))
			Expect(object.GetTemplate()).To(Equal("my-ci-template-id"))
			Expect(object.GetPublished()).To(BeTrue())
			Expect(object.GetTenant()).To(Equal("my-tenant"))
		})

		It("List objects", func() {
			const count = 10
			for i := range count {
				_, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
					Object: privatev1.ComputeInstanceCatalogItem_builder{
						Title:    fmt.Sprintf("CI catalog item %d", i),
						Template: "my-ci-template-id",
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			response, err := server.List(ctx, privatev1.ComputeInstanceCatalogItemsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			const count = 10
			for i := range count {
				_, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
					Object: privatev1.ComputeInstanceCatalogItem_builder{
						Title:    fmt.Sprintf("CI catalog item %d", i),
						Template: "my-ci-template-id",
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			response, err := server.List(ctx, privatev1.ComputeInstanceCatalogItemsListRequest_builder{
				Limit: new(int32(1)),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with filter", func() {
			const count = 10
			var objects []*privatev1.ComputeInstanceCatalogItem
			for i := range count {
				createResponse, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
					Object: privatev1.ComputeInstanceCatalogItem_builder{
						Title:    fmt.Sprintf("CI catalog item %d", i),
						Template: "my-ci-template-id",
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				objects = append(objects, createResponse.GetObject())
			}
			DeferCleanup(func() {
				for _, object := range objects {
					_, err := server.Delete(ctx, privatev1.ComputeInstanceCatalogItemsDeleteRequest_builder{
						Id: object.GetId(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())
				}
			})

			for _, object := range objects {
				getResponse, err := server.List(ctx, privatev1.ComputeInstanceCatalogItemsListRequest_builder{
					Filter: new(fmt.Sprintf("this.id == '%s'", object.GetId())),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(getResponse.GetSize()).To(BeNumerically("==", 1))
				Expect(getResponse.GetItems()[0].GetId()).To(Equal(object.GetId()))
			}
		})

		It("Get object", func() {
			createResponse, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Title:       "My CI catalog item",
					Description: "My description.",
					Template:    "my-ci-template-id",
					Published:   true,
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			DeferCleanup(func() {
				_, err := server.Delete(ctx, privatev1.ComputeInstanceCatalogItemsDeleteRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			})

			getResponse, err := server.Get(ctx, privatev1.ComputeInstanceCatalogItemsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(createResponse.GetObject(), getResponse.GetObject())).To(BeTrue())
		})

		It("Update object", func() {
			createResponse, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Title:       "Original title",
					Description: "Original description.",
					Template:    "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			DeferCleanup(func() {
				_, err := server.Delete(ctx, privatev1.ComputeInstanceCatalogItemsDeleteRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			})

			updateResponse, err := server.Update(ctx, privatev1.ComputeInstanceCatalogItemsUpdateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Id:          object.GetId(),
					Title:       "Updated title",
					Description: "Updated description.",
					Template:    "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetTitle()).To(Equal("Updated title"))
			Expect(updateResponse.GetObject().GetDescription()).To(Equal("Updated description."))

			getResponse, err := server.Get(ctx, privatev1.ComputeInstanceCatalogItemsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetTitle()).To(Equal("Updated title"))
			Expect(getResponse.GetObject().GetDescription()).To(Equal("Updated description."))
		})

		It("Update published using field mask", func() {
			createResponse, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Title:     "My CI catalog item",
					Template:  "my-ci-template-id",
					Published: false,
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			DeferCleanup(func() {
				_, err := server.Delete(ctx, privatev1.ComputeInstanceCatalogItemsDeleteRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			})

			updateResponse, err := server.Update(ctx, privatev1.ComputeInstanceCatalogItemsUpdateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Id:        object.GetId(),
					Published: true,
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"published"},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetPublished()).To(BeTrue())

			getResponse, err := server.Get(ctx, privatev1.ComputeInstanceCatalogItemsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetPublished()).To(BeTrue())
		})

		It("Creates object with field definitions and round-trips them", func() {
			response, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Title:    "CI catalog item with fields",
					Template: "my-ci-template-id",
					FieldDefinitions: []*privatev1.FieldDefinition{
						privatev1.FieldDefinition_builder{
							Path:             "spec.cores",
							DisplayName:      "CPU cores",
							Editable:         true,
							ValidationSchema: `{"type":"number","minimum":1,"maximum":128}`,
						}.Build(),
						privatev1.FieldDefinition_builder{
							Path:        "spec.memory_gib",
							DisplayName: "Memory (GiB)",
							Editable:    false,
						}.Build(),
					},
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()
			DeferCleanup(func() {
				_, err := server.Delete(ctx, privatev1.ComputeInstanceCatalogItemsDeleteRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			})

			getResponse, err := server.Get(ctx, privatev1.ComputeInstanceCatalogItemsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			fetched := getResponse.GetObject()
			Expect(fetched.GetFieldDefinitions()).To(HaveLen(2))

			fd0 := fetched.GetFieldDefinitions()[0]
			Expect(fd0.GetPath()).To(Equal("spec.cores"))
			Expect(fd0.GetDisplayName()).To(Equal("CPU cores"))
			Expect(fd0.GetEditable()).To(BeTrue())
			Expect(fd0.GetValidationSchema()).To(Equal(`{"type":"number","minimum":1,"maximum":128}`))

			fd1 := fetched.GetFieldDefinitions()[1]
			Expect(fd1.GetPath()).To(Equal("spec.memory_gib"))
			Expect(fd1.GetDisplayName()).To(Equal("Memory (GiB)"))
			Expect(fd1.GetEditable()).To(BeFalse())
		})

		It("Delete object", func() {
			createResponse, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Finalizers: []string{"a"},
					}.Build(),
					Title:    "My CI catalog item",
					Template: "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			_, err = server.Delete(ctx, privatev1.ComputeInstanceCatalogItemsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := server.Get(ctx, privatev1.ComputeInstanceCatalogItemsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})

		It("Blocks delete when referenced by a compute instance", func() {
			createResponse, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Title:     "Referenced CI catalog item",
					Template:  "my-ci-template-id",
					Published: true,
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			catalogItem := createResponse.GetObject()

			ciDao, err := dao.NewGenericDAO[*privatev1.ComputeInstance]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			_, err = ciDao.Create().SetObject(
				privatev1.ComputeInstance_builder{
					Metadata: privatev1.Metadata_builder{
						Name:   "ref-ci",
						Tenant: "system",
					}.Build(),
					Spec: privatev1.ComputeInstanceSpec_builder{
						CatalogItem: catalogItem.GetId(),
						Template:    "my-ci-template-id",
					}.Build(),
				}.Build(),
			).Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			_, err = server.Delete(ctx, privatev1.ComputeInstanceCatalogItemsDeleteRequest_builder{
				Id: catalogItem.GetId(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(status.Message()).To(ContainSubstring("still referenced"))

			getResponse, err := server.Get(ctx, privatev1.ComputeInstanceCatalogItemsGetRequest_builder{
				Id: catalogItem.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject()).ToNot(BeNil())
			Expect(getResponse.GetObject().GetMetadata().GetDeletionTimestamp()).To(BeNil())
		})

		It("Returns error when reference check fails during delete", func() {
			createResponse, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Title:    "CI catalog item with failing check",
					Template: "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			catalogItem := createResponse.GetObject()

			mockCtrl := gomock.NewController(GinkgoT())
			mockChecker := NewMockCatalogItemReferenceChecker(mockCtrl)
			mockChecker.EXPECT().hasReference(gomock.Any(), catalogItem.GetId()).
				Return(false, fmt.Errorf("database connection lost"))
			server.referenceChecker = mockChecker

			_, err = server.Delete(ctx, privatev1.ComputeInstanceCatalogItemsDeleteRequest_builder{
				Id: catalogItem.GetId(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("database connection lost"))
		})

		It("Rejects duplicate name within same tenant", func() {
			_, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "dev-sandbox",
					}.Build(),
					Title:    "First CI catalog item",
					Template: "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "dev-sandbox",
					}.Build(),
					Title:    "Second CI catalog item",
					Template: "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.AlreadyExists))
			Expect(status.Message()).To(ContainSubstring("dev-sandbox"))
		})

		It("Allows same name across different tenants", func() {
			_, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Name:   "dev-sandbox",
						Tenant: "system",
					}.Build(),
					Title:    "CI catalog item for system tenant",
					Template: "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Name:   "dev-sandbox",
						Tenant: "shared",
					}.Build(),
					Title:    "CI catalog item for shared tenant",
					Template: "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Rejects update to duplicate name within same tenant", func() {
			_, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "first-item",
					}.Build(),
					Title:    "First CI catalog item",
					Template: "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			secondResponse, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "second-item",
					}.Build(),
					Title:    "Second CI catalog item",
					Template: "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = server.Update(ctx, privatev1.ComputeInstanceCatalogItemsUpdateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Id: secondResponse.GetObject().GetId(),
					Metadata: privatev1.Metadata_builder{
						Name: "first-item",
					}.Build(),
					Title:    "Second CI catalog item",
					Template: "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.AlreadyExists))
			Expect(status.Message()).To(ContainSubstring("first-item"))
		})

		It("Allows empty name without conflict", func() {
			_, err := server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Title:    "First unnamed CI item",
					Template: "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Title:    "Second unnamed CI item",
					Template: "my-ci-template-id",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
