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
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Public IPs server", func() {
	var (
		ctx             context.Context
		tx              database.Tx
		publicIPPoolDao *dao.GenericDAO[*privatev1.PublicIPPool]
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
		err = dao.CreateTables[*privatev1.PublicIP](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.PublicIPPool](ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create the PublicIPPool DAO:
		publicIPPoolDao, err = dao.NewGenericDAO[*privatev1.PublicIPPool]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	// createReadyPool creates a READY pool with available capacity.
	createReadyPool := func(ctx context.Context) string {
		resp, err := publicIPPoolDao.Create().SetObject(
			privatev1.PublicIPPool_builder{
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"shared"},
				}.Build(),
				Spec: privatev1.PublicIPPoolSpec_builder{
					Cidrs: []string{"10.0.0.0/24"},
				}.Build(),
				Status: privatev1.PublicIPPoolStatus_builder{
					State:     privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY,
					Total:     254,
					Allocated: 0,
					Available: 254,
				}.Build(),
			}.Build(),
		).Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		return resp.GetObject().GetId()
	}

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPublicIPsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPublicIPsServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var (
			publicIPsServer *PublicIPsServer
			poolID          string
		)

		BeforeEach(func() {
			var err error

			// Create a READY pool for all behaviour tests:
			poolID = createReadyPool(ctx)

			// Create the server:
			publicIPsServer, err = NewPublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Creates object", func() {
			response, err := publicIPsServer.Create(ctx, publicv1.PublicIPsCreateRequest_builder{
				Object: publicv1.PublicIP_builder{
					Spec: publicv1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
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
			for range count {
				_, err := publicIPsServer.Create(ctx, publicv1.PublicIPsCreateRequest_builder{
					Object: publicv1.PublicIP_builder{
						Spec: publicv1.PublicIPSpec_builder{
							Pool: poolID,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := publicIPsServer.List(ctx, publicv1.PublicIPsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			// Create a few objects:
			const count = 10
			for range count {
				_, err := publicIPsServer.Create(ctx, publicv1.PublicIPsCreateRequest_builder{
					Object: publicv1.PublicIP_builder{
						Spec: publicv1.PublicIPSpec_builder{
							Pool: poolID,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := publicIPsServer.List(ctx, publicv1.PublicIPsListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			// Create a few objects:
			const count = 10
			for range count {
				_, err := publicIPsServer.Create(ctx, publicv1.PublicIPsCreateRequest_builder{
					Object: publicv1.PublicIP_builder{
						Spec: publicv1.PublicIPSpec_builder{
							Pool: poolID,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := publicIPsServer.List(ctx, publicv1.PublicIPsListRequest_builder{
				Offset: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-1))
		})

		It("List objects with filter", func() {
			// Create a few objects:
			const count = 10
			var objects []*publicv1.PublicIP
			for range count {
				response, err := publicIPsServer.Create(ctx, publicv1.PublicIPsCreateRequest_builder{
					Object: publicv1.PublicIP_builder{
						Spec: publicv1.PublicIPSpec_builder{
							Pool: poolID,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				objects = append(objects, response.GetObject())
			}

			// List the objects:
			for _, object := range objects {
				response, err := publicIPsServer.List(ctx, publicv1.PublicIPsListRequest_builder{
					Filter: proto.String(fmt.Sprintf("this.id == '%s'", object.GetId())),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 1))
				Expect(response.GetItems()[0].GetId()).To(Equal(object.GetId()))
			}
		})

		It("Get object", func() {
			// Create the object:
			createResponse, err := publicIPsServer.Create(ctx, publicv1.PublicIPsCreateRequest_builder{
				Object: publicv1.PublicIP_builder{
					Spec: publicv1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get it:
			getResponse, err := publicIPsServer.Get(ctx, publicv1.PublicIPsGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(createResponse.GetObject(), getResponse.GetObject())).To(BeTrue())
		})

		It("Delete object", func() {
			// Create the object:
			createResponse, err := publicIPsServer.Create(ctx, publicv1.PublicIPsCreateRequest_builder{
				Object: publicv1.PublicIP_builder{
					Spec: publicv1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Add a finalizer and transition to ALLOCATED. Finalizer prevents immediate
			// archival so we can verify the deletion timestamp. State must be ALLOCATED
			// because only ALLOCATED PublicIPs can be deleted. Both are set via raw SQL
			// because the public API doesn't expose finalizers or status.state.
			_, err = tx.Exec(
				ctx,
				`update public_ips set finalizers = '{"a"}',`+
					` data = jsonb_set(data, '{status,state}', '"PUBLIC_IP_STATE_ALLOCATED"') where id = $1`,
				object.GetId(),
			)
			Expect(err).ToNot(HaveOccurred())

			// Delete the object:
			_, err = publicIPsServer.Delete(ctx, publicv1.PublicIPsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get and verify:
			getResponse, err := publicIPsServer.Get(ctx, publicv1.PublicIPsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})
	})
})
