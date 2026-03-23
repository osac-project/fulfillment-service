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

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Network classes server", func() {
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
		err = dao.CreateTables[*publicv1.NetworkClass](ctx)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewNetworkClassesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewNetworkClassesServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewNetworkClassesServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewNetworkClassesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var (
			publicServer  *NetworkClassesServer
			privateServer *PrivateNetworkClassesServer
		)

		BeforeEach(func() {
			var err error

			// Create the public server:
			publicServer, err = NewNetworkClassesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create a private server for test data setup (private API requires
			// implementation_strategy which is not exposed in public API):
			privateServer, err = NewPrivateNetworkClassesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		// createNetworkClass creates a NetworkClass via the private server (which accepts
		// implementation_strategy) and returns the created object.
		createNetworkClass := func() *privatev1.NetworkClass {
			response, err := privateServer.Create(ctx, privatev1.NetworkClassesCreateRequest_builder{
				Object: privatev1.NetworkClass_builder{
					Title:                  "Test Network Class",
					ImplementationStrategy: "ovn-kubernetes",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			return response.GetObject()
		}

		It("List objects", func() {
			// Create a few objects via the private server:
			const count = 10
			for range count {
				createNetworkClass()
			}

			// List the objects via public server:
			response, err := publicServer.List(ctx, publicv1.NetworkClassesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			// Create a few objects via the private server:
			const count = 10
			for range count {
				createNetworkClass()
			}

			// List the objects via public server:
			response, err := publicServer.List(ctx, publicv1.NetworkClassesListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			// Create a few objects via the private server:
			const count = 10
			for range count {
				createNetworkClass()
			}

			// List the objects via public server:
			response, err := publicServer.List(ctx, publicv1.NetworkClassesListRequest_builder{
				Offset: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-1))
		})

		It("List objects with filter", func() {
			// Create a few objects via the private server:
			const count = 10
			var ids []string
			for range count {
				obj := createNetworkClass()
				ids = append(ids, obj.GetId())
			}

			// List the objects via public server:
			for _, id := range ids {
				response, err := publicServer.List(ctx, publicv1.NetworkClassesListRequest_builder{
					Filter: proto.String(fmt.Sprintf("this.id == '%s'", id)),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 1))
				Expect(response.GetItems()[0].GetId()).To(Equal(id))
			}
		})

		It("Get object", func() {
			// Create the object via the private server:
			privateObj := createNetworkClass()

			// Get it via public server:
			getResponse, err := publicServer.Get(ctx, publicv1.NetworkClassesGetRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			publicObj := getResponse.GetObject()
			Expect(publicObj.GetId()).To(Equal(privateObj.GetId()))
			Expect(publicObj.GetTitle()).To(Equal(privateObj.GetTitle()))
		})

		It("Update object", func() {
			// Create the object via the private server:
			privateObj := createNetworkClass()

			// Update the object via public server:
			updateResponse, err := publicServer.Update(ctx, publicv1.NetworkClassesUpdateRequest_builder{
				Object: publicv1.NetworkClass_builder{
					Id:          privateObj.GetId(),
					Title:       "Your title",
					Description: "Your description.",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetTitle()).To(Equal("Your title"))
			Expect(updateResponse.GetObject().GetDescription()).To(Equal("Your description."))

			// Get and verify via public server:
			getResponse, err := publicServer.Get(ctx, publicv1.NetworkClassesGetRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetTitle()).To(Equal("Your title"))
			Expect(getResponse.GetObject().GetDescription()).To(Equal("Your description."))
		})

		It("Delete object", func() {
			// Create the object via the private server:
			privateObj := createNetworkClass()

			// Add a finalizer, as otherwise the object will be immediatelly deleted and archived and it
			// won't be possible to verify the deletion timestamp. This can't be done using the server
			// because this is a public object, and public objects don't have the finalizers field.
			_, err := tx.Exec(
				ctx,
				`update network_classes set finalizers = '{"a"}' where id = $1`,
				privateObj.GetId(),
			)
			Expect(err).ToNot(HaveOccurred())

			// Delete the object via public server:
			_, err = publicServer.Delete(ctx, publicv1.NetworkClassesDeleteRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get and verify via public server:
			getResponse, err := publicServer.Get(ctx, publicv1.NetworkClassesGetRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := getResponse.GetObject()
			Expect(object.GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})
	})
})
