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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
)

var _ = Describe("Public IP pools server", func() {
	var (
		ctx         context.Context
		tx          database.Tx
		privatePool *PrivatePublicIPPoolsServer
	)

	BeforeEach(func() {
		var err error

		ctx = context.Background()

		// Prepare the database pool:
		db, err := server.NewInstance().Build()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(db.Close)
		pool, err := db.Pool(ctx)
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
			err := tx.End(ctx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)

		// The private server is used to seed pool data for list/get tests.
		privatePool, err = NewPrivatePublicIPPoolsServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Builder", func() {
		It("Builds successfully with required parameters", func() {
			s, err := NewPublicIPPoolsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(s).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			s, err := NewPublicIPPoolsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(s).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			s, err := NewPublicIPPoolsServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("attribution logic is mandatory"))
			Expect(s).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			s, err := NewPublicIPPoolsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(s).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var publicPool *PublicIPPoolsServer

		BeforeEach(func() {
			var err error
			publicPool, err = NewPublicIPPoolsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		// makePool creates a pool via the private server in the given state.
		makePool := func(name, cidr string, state privatev1.PublicIPPoolState) *privatev1.PublicIPPool {
			createResp, err := privatePool.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
				Object: privatev1.PublicIPPool_builder{
					Metadata: privatev1.Metadata_builder{Name: name}.Build(),
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{cidr},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			pool := createResp.GetObject()
			pool.GetStatus().SetState(state)
			pool.GetStatus().SetAvailable(256)
			updateResp, err := privatePool.Update(ctx, privatev1.PublicIPPoolsUpdateRequest_builder{
				Object: pool,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			return updateResp.GetObject()
		}

		It("Lists only ready pools with available capacity", func() {
			const count = 3
			for i := range count {
				makePool(fmt.Sprintf("pool-%d", i), fmt.Sprintf("10.%d.0.0/24", i),
					privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY)
			}

			response, err := publicPool.List(ctx, publicv1.PublicIPPoolsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(Equal(int32(count)))
			Expect(response.GetTotal()).To(Equal(int32(count)))
			Expect(response.GetItems()).To(HaveLen(count))
		})

		It("Does not list pools in UNSPECIFIED state", func() {
			makePool("unspecified-pool", "10.0.0.0/24", privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_UNSPECIFIED)

			response, err := publicPool.List(ctx, publicv1.PublicIPPoolsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(BeEmpty())
		})

		It("Does not list pools in PENDING state", func() {
			makePool("pending-pool", "10.0.0.0/24", privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_PENDING)

			response, err := publicPool.List(ctx, publicv1.PublicIPPoolsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(BeEmpty())
		})

		It("Does not list pools with no available capacity", func() {
			createResp, err := privatePool.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
				Object: privatev1.PublicIPPool_builder{
					Metadata: privatev1.Metadata_builder{Name: "full-pool"}.Build(),
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"10.0.0.0/24"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			pool := createResp.GetObject()
			pool.GetStatus().SetState(privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY)
			pool.GetStatus().SetAvailable(0)
			_, err = privatePool.Update(ctx, privatev1.PublicIPPoolsUpdateRequest_builder{
				Object: pool,
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			response, err := publicPool.List(ctx, publicv1.PublicIPPoolsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(BeEmpty())
		})

		It("Lists ready pools with limit and correct total", func() {
			const count = 5
			for i := range count {
				makePool(fmt.Sprintf("pool-%d", i), fmt.Sprintf("10.%d.0.0/24", i),
					privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY)
			}

			response, err := publicPool.List(ctx, publicv1.PublicIPPoolsListRequest_builder{
				Limit: proto.Int32(2),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(2))
			Expect(response.GetTotal()).To(Equal(int32(count)))
		})

		It("Lists ready pools with offset and limit", func() {
			const count = 5
			for i := range count {
				makePool(fmt.Sprintf("pool-%d", i), fmt.Sprintf("10.%d.0.0/24", i),
					privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY)
			}

			response, err := publicPool.List(ctx, publicv1.PublicIPPoolsListRequest_builder{
				Offset: proto.Int32(2),
				Limit:  proto.Int32(2),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(2))
			Expect(response.GetSize()).To(Equal(int32(2)))
			Expect(response.GetTotal()).To(Equal(int32(count)))
		})

		It("Gets a ready pool by ID", func() {
			pool := makePool("my-pool", "192.168.1.0/24",
				privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY)

			getResp, err := publicPool.Get(ctx, publicv1.PublicIPPoolsGetRequest_builder{
				Id: pool.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResp.GetObject().GetId()).To(Equal(pool.GetId()))
			Expect(getResp.GetObject().GetMetadata().GetName()).To(Equal("my-pool"))
			Expect(getResp.GetObject().GetSpec().GetCidrs()).To(ConsistOf("192.168.1.0/24"))
			Expect(getResp.GetObject().GetSpec().GetIpFamily()).To(Equal(publicv1.IPFamily_IP_FAMILY_IPV4))
		})

		It("Returns not found for pool in PENDING state", func() {
			pool := makePool("pending-pool", "10.0.0.0/24",
				privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_PENDING)

			_, err := publicPool.Get(ctx, publicv1.PublicIPPoolsGetRequest_builder{
				Id: pool.GetId(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(grpcstatus.Code(err)).To(Equal(grpccodes.NotFound))
		})

		It("Returns not found for non-existent pool", func() {
			_, err := publicPool.Get(ctx, publicv1.PublicIPPoolsGetRequest_builder{
				Id: "non-existent-id",
			}.Build())
			Expect(err).To(HaveOccurred())
		})

		It("Returns empty list when no pools exist", func() {
			response, err := publicPool.List(ctx, publicv1.PublicIPPoolsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(BeEmpty())
			Expect(response.GetTotal()).To(Equal(int32(0)))
		})
	})
})
