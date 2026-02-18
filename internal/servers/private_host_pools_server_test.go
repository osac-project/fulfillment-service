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

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Private host pools server", func() {
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
		err = dao.CreateTables(ctx, "host_pools")
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivateHostPoolsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivateHostPoolsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPrivateHostPoolsServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivateHostPoolsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var server *PrivateHostPoolsServer

		BeforeEach(func() {
			var err error

			// Create the server:
			server, err = NewPrivateHostPoolsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Creates object", func() {
			response, err := server.Create(ctx, privatev1.HostPoolsCreateRequest_builder{
				Object: privatev1.HostPool_builder{
					Spec: privatev1.HostPoolSpec_builder{
						HostSets: map[string]*privatev1.HostPoolHostSet{
							"set1": privatev1.HostPoolHostSet_builder{
								HostClass: "class1",
								Size:      3,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())
			Expect(object.GetSpec().GetHostSets()).To(HaveKey("set1"))
		})

		It("List objects", func() {
			// Create a few objects:
			const count = 10
			for i := range count {
				_, err := server.Create(ctx, privatev1.HostPoolsCreateRequest_builder{
					Object: privatev1.HostPool_builder{
						Spec: privatev1.HostPoolSpec_builder{
							HostSets: map[string]*privatev1.HostPoolHostSet{
								fmt.Sprintf("set_%d", i): privatev1.HostPoolHostSet_builder{
									HostClass: fmt.Sprintf("class_%d", i),
									Size:      int32(i + 1),
								}.Build(),
							},
						}.Build(),
						Status: privatev1.HostPoolStatus_builder{
							State: privatev1.HostPoolState_HOST_POOL_STATE_READY,
							Hosts: []string{fmt.Sprintf("host_%d", i)},
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, privatev1.HostPoolsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			// Create a few objects:
			const count = 10
			for i := range count {
				_, err := server.Create(ctx, privatev1.HostPoolsCreateRequest_builder{
					Object: privatev1.HostPool_builder{
						Spec: privatev1.HostPoolSpec_builder{
							HostSets: map[string]*privatev1.HostPoolHostSet{
								fmt.Sprintf("set_%d", i): privatev1.HostPoolHostSet_builder{
									HostClass: fmt.Sprintf("class_%d", i),
									Size:      int32(i + 1),
								}.Build(),
							},
						}.Build(),
						Status: privatev1.HostPoolStatus_builder{
							State: privatev1.HostPoolState_HOST_POOL_STATE_READY,
							Hosts: []string{fmt.Sprintf("host_%d", i)},
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, privatev1.HostPoolsListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			// Create a few objects:
			const count = 10
			for i := range count {
				_, err := server.Create(ctx, privatev1.HostPoolsCreateRequest_builder{
					Object: privatev1.HostPool_builder{
						Spec: privatev1.HostPoolSpec_builder{
							HostSets: map[string]*privatev1.HostPoolHostSet{
								fmt.Sprintf("set_%d", i): privatev1.HostPoolHostSet_builder{
									HostClass: fmt.Sprintf("class_%d", i),
									Size:      int32(i + 1),
								}.Build(),
							},
						}.Build(),
						Status: privatev1.HostPoolStatus_builder{
							State: privatev1.HostPoolState_HOST_POOL_STATE_READY,
							Hosts: []string{fmt.Sprintf("host_%d", i)},
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, privatev1.HostPoolsListRequest_builder{
				Offset: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-1))
		})

		It("List objects with filter", func() {
			// Create a few objects:
			const count = 10
			var objects []*privatev1.HostPool
			for i := range count {
				response, err := server.Create(ctx, privatev1.HostPoolsCreateRequest_builder{
					Object: privatev1.HostPool_builder{
						Spec: privatev1.HostPoolSpec_builder{
							HostSets: map[string]*privatev1.HostPoolHostSet{
								fmt.Sprintf("set_%d", i): privatev1.HostPoolHostSet_builder{
									HostClass: fmt.Sprintf("class_%d", i),
									Size:      int32(i + 1),
								}.Build(),
							},
						}.Build(),
						Status: privatev1.HostPoolStatus_builder{
							State: privatev1.HostPoolState_HOST_POOL_STATE_READY,
							Hosts: []string{fmt.Sprintf("host_%d", i)},
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				objects = append(objects, response.GetObject())
			}

			// List the objects:
			for _, object := range objects {
				response, err := server.List(ctx, privatev1.HostPoolsListRequest_builder{
					Filter: proto.String(fmt.Sprintf("this.id == '%s'", object.GetId())),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 1))
				Expect(response.GetItems()[0].GetId()).To(Equal(object.GetId()))
			}
		})

		It("Get object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.HostPoolsCreateRequest_builder{
				Object: privatev1.HostPool_builder{
					Spec: privatev1.HostPoolSpec_builder{
						HostSets: map[string]*privatev1.HostPoolHostSet{
							"test_set": privatev1.HostPoolHostSet_builder{
								HostClass: "test_class",
								Size:      5,
							}.Build(),
						},
					}.Build(),
					Status: privatev1.HostPoolStatus_builder{
						State: privatev1.HostPoolState_HOST_POOL_STATE_PROGRESSING,
						Hosts: []string{"host1", "host2", "host3"},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get it:
			getResponse, err := server.Get(ctx, privatev1.HostPoolsGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(createResponse.GetObject(), getResponse.GetObject())).To(BeTrue())
		})

		It("Update object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.HostPoolsCreateRequest_builder{
				Object: privatev1.HostPool_builder{
					Spec: privatev1.HostPoolSpec_builder{
						HostSets: map[string]*privatev1.HostPoolHostSet{
							"original_set": privatev1.HostPoolHostSet_builder{
								HostClass: "original_class",
								Size:      3,
							}.Build(),
						},
					}.Build(),
					Status: privatev1.HostPoolStatus_builder{
						State: privatev1.HostPoolState_HOST_POOL_STATE_PROGRESSING,
						Hosts: []string{"host1"},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Update the object:
			updateResponse, err := server.Update(ctx, privatev1.HostPoolsUpdateRequest_builder{
				Object: privatev1.HostPool_builder{
					Id: object.GetId(),
					Spec: privatev1.HostPoolSpec_builder{
						HostSets: map[string]*privatev1.HostPoolHostSet{
							"updated_set": privatev1.HostPoolHostSet_builder{
								HostClass: "updated_class",
								Size:      5,
							}.Build(),
						},
					}.Build(),
					Status: privatev1.HostPoolStatus_builder{
						State: privatev1.HostPoolState_HOST_POOL_STATE_READY,
						Hosts: []string{"host1", "host2", "host3", "host4", "host5"},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetSpec().GetHostSets()).To(HaveKey("updated_set"))
			Expect(updateResponse.GetObject().GetStatus().GetState()).To(Equal(privatev1.HostPoolState_HOST_POOL_STATE_READY))
			Expect(updateResponse.GetObject().GetStatus().GetHosts()).To(HaveLen(5))

			// Get and verify:
			getResponse, err := server.Get(ctx, privatev1.HostPoolsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetSpec().GetHostSets()).To(HaveKey("updated_set"))
			Expect(getResponse.GetObject().GetStatus().GetState()).To(Equal(privatev1.HostPoolState_HOST_POOL_STATE_READY))
			Expect(getResponse.GetObject().GetStatus().GetHosts()).To(HaveLen(5))
		})

		It("Delete object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.HostPoolsCreateRequest_builder{
				Object: privatev1.HostPool_builder{
					Metadata: privatev1.Metadata_builder{
						Finalizers: []string{"a"},
					}.Build(),
					Spec: privatev1.HostPoolSpec_builder{
						HostSets: map[string]*privatev1.HostPoolHostSet{
							"test_set": privatev1.HostPoolHostSet_builder{
								HostClass: "test_class",
								Size:      3,
							}.Build(),
						},
					}.Build(),
					Status: privatev1.HostPoolStatus_builder{
						State: privatev1.HostPoolState_HOST_POOL_STATE_READY,
						Hosts: []string{"host1"},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Delete the object:
			_, err = server.Delete(ctx, privatev1.HostPoolsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get and verify:
			getResponse, err := server.Get(ctx, privatev1.HostPoolsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})
	})
})
