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
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Private cluster templates server", func() {
	var (
		ctx context.Context
		tx  database.Tx
	)

	BeforeEach(func() {
		var err error

		// Create a context:
		ctx = context.Background()

		// Prepare the database pool:
		db := server.MakeDatabase()
		DeferCleanup(db.Close)
		pool, err := pgxpool.New(ctx, db.MakeURL())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)

		// Create the transaction manager:
		tm, err := database.NewTxManager().
			SetLogger(logger).
			SetPool(pool).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Start a transaction and add it to the context:
		tx, err = tm.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := tm.End(ctx, tx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)

		// Create the tables:
		err = dao.CreateTables(ctx, "cluster_templates")
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivateClusterTemplatesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivateClusterTemplatesServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPrivateClusterTemplatesServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivateClusterTemplatesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var server *PrivateClusterTemplatesServer

		BeforeEach(func() {
			var err error

			// Create the server:
			server, err = NewPrivateClusterTemplatesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Creates object", func() {
			response, err := server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Title:       "My title",
					Description: "My description.",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())
		})

		It("List objects", func() {
			// Create a few objects:
			const count = 10
			for i := range count {
				_, err := server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
					Object: privatev1.ClusterTemplate_builder{
						Title:       fmt.Sprintf("My title %d", i),
						Description: fmt.Sprintf("My description %d.", i),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, privatev1.ClusterTemplatesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			// Create a few objects:
			const count = 10
			for i := range count {
				_, err := server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
					Object: privatev1.ClusterTemplate_builder{
						Title:       fmt.Sprintf("My title %d", i),
						Description: fmt.Sprintf("My description %d.", i),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, privatev1.ClusterTemplatesListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			// Create a few objects:
			const count = 10
			var objects []*privatev1.ClusterTemplate
			for i := range count {
				createResponse, err := server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
					Object: privatev1.ClusterTemplate_builder{
						Title:       fmt.Sprintf("My title %d", i),
						Description: fmt.Sprintf("My description %d.", i),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				objects = append(objects, createResponse.GetObject())
			}
			DeferCleanup(func() {
				for _, object := range objects {
					_, err := server.Delete(ctx, privatev1.ClusterTemplatesDeleteRequest_builder{
						Id: object.GetId(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())
				}
			})

			// List the objects:
			response, err := server.List(ctx, privatev1.ClusterTemplatesListRequest_builder{
				Offset: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-1))
		})

		It("List objects with filter", func() {
			// Create a few objects:
			const count = 10
			var objects []*privatev1.ClusterTemplate
			for i := range count {
				createResponse, err := server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
					Object: privatev1.ClusterTemplate_builder{
						Title:       fmt.Sprintf("My title %d", i),
						Description: fmt.Sprintf("My description %d.", i),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				objects = append(objects, createResponse.GetObject())
			}
			DeferCleanup(func() {
				for _, object := range objects {
					_, err := server.Delete(ctx, privatev1.ClusterTemplatesDeleteRequest_builder{
						Id: object.GetId(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())
				}
			})

			// List the objects:
			for _, object := range objects {
				getResponse, err := server.List(ctx, privatev1.ClusterTemplatesListRequest_builder{
					Filter: proto.String(fmt.Sprintf("this.id == '%s'", object.GetId())),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(getResponse.GetSize()).To(BeNumerically("==", 1))
				Expect(getResponse.GetItems()[0].GetId()).To(Equal(object.GetId()))
			}
		})

		It("Get object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Title:       "My title",
					Description: "My description.",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			DeferCleanup(func() {
				_, err := server.Delete(ctx, privatev1.ClusterTemplatesDeleteRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			})

			// Get it:
			getResponse, err := server.Get(ctx, privatev1.ClusterTemplatesGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(createResponse.GetObject(), getResponse.GetObject())).To(BeTrue())
		})

		It("Update object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Title:       "My title",
					Description: "My description.",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			DeferCleanup(func() {
				_, err := server.Delete(ctx, privatev1.ClusterTemplatesDeleteRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			})

			// Update the object:
			updateResponse, err := server.Update(ctx, privatev1.ClusterTemplatesUpdateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Id:          object.GetId(),
					Title:       "Your title",
					Description: "Your description.",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetTitle()).To(Equal("Your title"))
			Expect(updateResponse.GetObject().GetDescription()).To(Equal("Your description."))

			// Get and verify:
			getResponse, err := server.Get(ctx, privatev1.ClusterTemplatesGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetTitle()).To(Equal("Your title"))
			Expect(getResponse.GetObject().GetDescription()).To(Equal("Your description."))
		})

		It("Update title ony, using field mask", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Title:       "Original title",
					Description: "Original description.",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			DeferCleanup(func() {
				_, err := server.Delete(ctx, privatev1.ClusterTemplatesDeleteRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			})

			// Update the object:
			updateResponse, err := server.Update(ctx, privatev1.ClusterTemplatesUpdateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Id:          object.GetId(),
					Title:       "Updated title",
					Description: "Updated description.",
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"title",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			Expect(object.GetTitle()).To(Equal("Updated title"))
			Expect(object.GetDescription()).To(Equal("Original description."))

			// Get and verify:
			getResponse, err := server.Get(ctx, privatev1.ClusterTemplatesGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetTitle()).To(Equal("Updated title"))
			Expect(object.GetDescription()).To(Equal("Original description."))
		})

		It("Update description ony, using field mask", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Title:       "Original title",
					Description: "Original description.",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			DeferCleanup(func() {
				_, err := server.Delete(ctx, privatev1.ClusterTemplatesDeleteRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			})

			// Update the object:
			updateResponse, err := server.Update(ctx, privatev1.ClusterTemplatesUpdateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Id:          object.GetId(),
					Title:       "Updated title",
					Description: "Updated description.",
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"description",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			Expect(object.GetTitle()).To(Equal("Original title"))
			Expect(object.GetDescription()).To(Equal("Updated description."))

			// Get and verify:
			getResponse, err := server.Get(ctx, privatev1.ClusterTemplatesGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetTitle()).To(Equal("Original title"))
			Expect(object.GetDescription()).To(Equal("Updated description."))
		})

		It("Update title and description using field mask", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Title:       "Original title",
					Description: "Original description.",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			DeferCleanup(func() {
				_, err := server.Delete(ctx, privatev1.ClusterTemplatesDeleteRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			})

			// Update the object:
			updateResponse, err := server.Update(ctx, privatev1.ClusterTemplatesUpdateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Id:          object.GetId(),
					Title:       "Updated title",
					Description: "Updated description.",
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"description",
						"title",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			Expect(object.GetTitle()).To(Equal("Updated title"))
			Expect(object.GetDescription()).To(Equal("Updated description."))

			// Get and verify:
			getResponse, err := server.Get(ctx, privatev1.ClusterTemplatesGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetTitle()).To(Equal("Updated title"))
			Expect(object.GetDescription()).To(Equal("Updated description."))
		})

		It("Delete object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Metadata: privatev1.Metadata_builder{
						Finalizers: []string{"a"},
					}.Build(),
					Title:       "My title",
					Description: "My description.",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Delete the object:
			_, err = server.Delete(ctx, privatev1.ClusterTemplatesDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get and verify:
			getResponse, err := server.Get(ctx, privatev1.ClusterTemplatesGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})
	})
})
