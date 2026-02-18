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

	ffv1 "github.com/osac-project/fulfillment-service/internal/api/fulfillment/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Hosts server", func() {
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
		err = dao.CreateTables(ctx, "hosts")
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewHostsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewHostsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewHostsServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewHostsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var server *HostsServer

		BeforeEach(func() {
			var err error

			// Create the server:
			server, err = NewHostsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Creates object", func() {
			response, err := server.Create(ctx, ffv1.HostsCreateRequest_builder{
				Object: ffv1.Host_builder{
					Spec: ffv1.HostSpec_builder{
						PowerState: ffv1.HostPowerState_HOST_POWER_STATE_OFF,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())
			Expect(object.GetSpec().GetPowerState()).To(Equal(ffv1.HostPowerState_HOST_POWER_STATE_OFF))
		})

		It("List objects", func() {
			// Create a few objects:
			const count = 10
			for range count {
				_, err := server.Create(ctx, ffv1.HostsCreateRequest_builder{
					Object: ffv1.Host_builder{
						Spec: ffv1.HostSpec_builder{
							PowerState: ffv1.HostPowerState_HOST_POWER_STATE_ON,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, ffv1.HostsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			// Create a few objects:
			const count = 10
			for range count {
				_, err := server.Create(ctx, ffv1.HostsCreateRequest_builder{
					Object: ffv1.Host_builder{
						Spec: ffv1.HostSpec_builder{
							PowerState: ffv1.HostPowerState_HOST_POWER_STATE_ON,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, ffv1.HostsListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			// Create a few objects:
			const count = 10
			for range count {
				_, err := server.Create(ctx, ffv1.HostsCreateRequest_builder{
					Object: ffv1.Host_builder{
						Spec: ffv1.HostSpec_builder{
							PowerState: ffv1.HostPowerState_HOST_POWER_STATE_ON,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, ffv1.HostsListRequest_builder{
				Offset: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-1))
		})

		It("List objects with order", func() {
			// Create a few objects:
			const count = 5
			var objects []*ffv1.Host
			for range count {
				response, err := server.Create(ctx, ffv1.HostsCreateRequest_builder{
					Object: ffv1.Host_builder{
						Spec: ffv1.HostSpec_builder{
							PowerState: ffv1.HostPowerState_HOST_POWER_STATE_ON,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				objects = append(objects, response.GetObject())
			}

			// List the objects with order:
			response, err := server.List(ctx, ffv1.HostsListRequest_builder{
				Order: proto.String("this.id desc"),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count))
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with filter", func() {
			// Create a few objects:
			const count = 10
			var objects []*ffv1.Host
			for range count {
				response, err := server.Create(ctx, ffv1.HostsCreateRequest_builder{
					Object: ffv1.Host_builder{
						Spec: ffv1.HostSpec_builder{
							PowerState: ffv1.HostPowerState_HOST_POWER_STATE_ON,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				objects = append(objects, response.GetObject())
			}

			// List the objects:
			for _, object := range objects {
				response, err := server.List(ctx, ffv1.HostsListRequest_builder{
					Filter: proto.String(fmt.Sprintf("this.id == '%s'", object.GetId())),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 1))
				Expect(response.GetItems()[0].GetId()).To(Equal(object.GetId()))
			}
		})

		It("Get object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.HostsCreateRequest_builder{
				Object: ffv1.Host_builder{
					Spec: ffv1.HostSpec_builder{
						PowerState: ffv1.HostPowerState_HOST_POWER_STATE_OFF,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get it:
			getResponse, err := server.Get(ctx, ffv1.HostsGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(createResponse.GetObject(), getResponse.GetObject())).To(BeTrue())
		})

		It("Update object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.HostsCreateRequest_builder{
				Object: ffv1.Host_builder{
					Spec: ffv1.HostSpec_builder{
						PowerState: ffv1.HostPowerState_HOST_POWER_STATE_OFF,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Update the object:
			updateResponse, err := server.Update(ctx, ffv1.HostsUpdateRequest_builder{
				Object: ffv1.Host_builder{
					Id: object.GetId(),
					Spec: ffv1.HostSpec_builder{
						PowerState: ffv1.HostPowerState_HOST_POWER_STATE_ON,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetSpec().GetPowerState()).To(Equal(ffv1.HostPowerState_HOST_POWER_STATE_ON))

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.HostsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetSpec().GetPowerState()).To(Equal(ffv1.HostPowerState_HOST_POWER_STATE_ON))
		})

		It("Delete object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.HostsCreateRequest_builder{
				Object: ffv1.Host_builder{
					Spec: ffv1.HostSpec_builder{
						PowerState: ffv1.HostPowerState_HOST_POWER_STATE_OFF,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Add a finalizer, as otherwise the object will be immediately deleted and archived and it
			// won't be possible to verify the deletion timestamp. This can't be done using the server
			// because this is a public object, and public objects don't have the finalizers field.
			_, err = tx.Exec(
				ctx,
				`update hosts set finalizers = '{"a"}' where id = $1`,
				object.GetId(),
			)
			Expect(err).ToNot(HaveOccurred())

			// Delete the object:
			_, err = server.Delete(ctx, ffv1.HostsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.HostsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})

		It("Prevents status field updates", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.HostsCreateRequest_builder{
				Object: ffv1.Host_builder{
					Spec: ffv1.HostSpec_builder{
						PowerState: ffv1.HostPowerState_HOST_POWER_STATE_OFF,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Try to update including status field (should be ignored):
			updateResponse, err := server.Update(ctx, ffv1.HostsUpdateRequest_builder{
				Object: ffv1.Host_builder{
					Id: object.GetId(),
					Spec: ffv1.HostSpec_builder{
						PowerState: ffv1.HostPowerState_HOST_POWER_STATE_ON,
					}.Build(),
					Status: ffv1.HostStatus_builder{
						PowerState: ffv1.HostPowerState_HOST_POWER_STATE_ON,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// The spec should be updated but status should remain unchanged:
			Expect(updateResponse.GetObject().GetSpec().GetPowerState()).To(Equal(ffv1.HostPowerState_HOST_POWER_STATE_ON))
			// Status field should be ignored in updates from public API
		})
	})
})
