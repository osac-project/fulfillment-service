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
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Private public IP pools server", func() {
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
		err = dao.CreateTables[*privatev1.PublicIPPool](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.PublicIP](ctx)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivatePublicIPPoolsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivatePublicIPPoolsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPrivatePublicIPPoolsServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivatePublicIPPoolsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var (
			poolsServer *PrivatePublicIPPoolsServer
			ipsServer   *PrivatePublicIPsServer
		)

		BeforeEach(func() {
			var err error

			// Create the pools server:
			poolsServer, err = NewPrivatePublicIPPoolsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create the IPs server (used to seed PublicIP objects for deletion tests):
			ipsServer, err = NewPrivatePublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Creates a pool", func() {
			response, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
				Object: privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"192.168.1.0/24"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object.GetId()).ToNot(BeEmpty())
			Expect(object.GetSpec().GetCidrs()).To(ConsistOf("192.168.1.0/24"))
			Expect(object.GetSpec().GetIpFamily()).To(Equal(privatev1.IPFamily_IP_FAMILY_IPV4))
		})

		It("Lists pools", func() {
			const count = 5
			for i := range count {
				_, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Metadata: privatev1.Metadata_builder{
							Name: fmt.Sprintf("pool-%d", i),
						}.Build(),
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{fmt.Sprintf("10.%d.0.0/24", i)},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			response, err := poolsServer.List(ctx, privatev1.PublicIPPoolsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(count))
		})

		It("Lists pools with limit", func() {
			const count = 5
			for i := range count {
				_, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{fmt.Sprintf("10.%d.0.0/24", i)},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			response, err := poolsServer.List(ctx, privatev1.PublicIPPoolsListRequest_builder{
				Limit: proto.Int32(2),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 2))
		})

		It("Lists pools with filter", func() {
			createResponse, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
				Object: privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"192.168.1.0/24"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			id := createResponse.GetObject().GetId()

			response, err := poolsServer.List(ctx, privatev1.PublicIPPoolsListRequest_builder{
				Filter: proto.String(fmt.Sprintf("this.id == '%s'", id)),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
			Expect(response.GetItems()[0].GetId()).To(Equal(id))
		})

		It("Gets a pool", func() {
			createResponse, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
				Object: privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"192.168.1.0/24"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := poolsServer.Get(ctx, privatev1.PublicIPPoolsGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(createResponse.GetObject(), getResponse.GetObject())).To(BeTrue())
		})

		It("Updates a pool", func() {
			createResponse, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
				Object: privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"192.168.1.0/24"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			updateResponse, err := poolsServer.Update(ctx, privatev1.PublicIPPoolsUpdateRequest_builder{
				Object: privatev1.PublicIPPool_builder{
					Id: object.GetId(),
					Metadata: privatev1.Metadata_builder{
						Name: "my-pool",
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"metadata.name"},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetMetadata().GetName()).To(Equal("my-pool"))
		})

		It("Deletes a pool", func() {
			createResponse, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
				Object: privatev1.PublicIPPool_builder{
					Metadata: privatev1.Metadata_builder{
						Finalizers: []string{"test"},
					}.Build(),
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"192.168.1.0/24"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			_, err = poolsServer.Delete(ctx, privatev1.PublicIPPoolsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := poolsServer.Get(ctx, privatev1.PublicIPPoolsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})

		It("Rejects deletion when allocated PublicIPs reference the pool", func() {
			// Create a pool:
			createPoolResponse, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
				Object: privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"10.0.0.0/24"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			poolID := createPoolResponse.GetObject().GetId()

			// Allocate a PublicIP from the pool:
			_, err = ipsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Attempt to delete the pool — must fail:
			_, err = poolsServer.Delete(ctx, privatev1.PublicIPPoolsDeleteRequest_builder{
				Id: poolID,
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("public IP(s) are still allocated"))
		})

		It("Allows deletion when no PublicIPs reference the pool", func() {
			// Create a pool:
			createPoolResponse, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
				Object: privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"10.1.0.0/24"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			poolID := createPoolResponse.GetObject().GetId()

			// No PublicIPs reference this pool — deletion must succeed:
			_, err = poolsServer.Delete(ctx, privatev1.PublicIPPoolsDeleteRequest_builder{
				Id: poolID,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
