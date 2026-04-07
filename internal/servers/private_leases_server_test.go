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
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Private leases server", func() {
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
		err = dao.CreateTables[*privatev1.Lease](ctx)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivateLeasesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivateLeasesServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPrivateLeasesServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivateLeasesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var leasesServer *PrivateLeasesServer

		BeforeEach(func() {
			var err error

			// Create the server:
			leasesServer, err = NewPrivateLeasesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Creates a lease", func() {
			response, err := leasesServer.Create(ctx, privatev1.LeasesCreateRequest_builder{
				Object: privatev1.Lease_builder{
					Spec: privatev1.LeaseSpec_builder{
						Holder:           "controller-0",
						Duration:         durationpb.New(15_000_000_000),
						AcquireTimestamp: timestamppb.Now(),
						RenewTimestamp:   timestamppb.Now(),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())
			Expect(object.GetSpec().GetHolder()).To(Equal("controller-0"))
		})

		It("Lists leases", func() {
			const count = 5
			for i := range count {
				_, err := leasesServer.Create(ctx, privatev1.LeasesCreateRequest_builder{
					Object: privatev1.Lease_builder{
						Metadata: privatev1.Metadata_builder{
							Name: fmt.Sprintf("lease-%d", i),
						}.Build(),
						Spec: privatev1.LeaseSpec_builder{
							Holder:   fmt.Sprintf("controller-%d", i),
							Duration: durationpb.New(15_000_000_000),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			response, err := leasesServer.List(ctx, privatev1.LeasesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.GetItems()).To(HaveLen(count))
		})

		It("Lists leases with limit", func() {
			const count = 5
			for i := range count {
				_, err := leasesServer.Create(ctx, privatev1.LeasesCreateRequest_builder{
					Object: privatev1.Lease_builder{
						Spec: privatev1.LeaseSpec_builder{
							Holder: fmt.Sprintf("controller-%d", i),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			response, err := leasesServer.List(ctx, privatev1.LeasesListRequest_builder{
				Limit: proto.Int32(2),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 2))
		})

		It("Lists leases with filter", func() {
			createResponse, err := leasesServer.Create(ctx, privatev1.LeasesCreateRequest_builder{
				Object: privatev1.Lease_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-lease",
					}.Build(),
					Spec: privatev1.LeaseSpec_builder{
						Holder: "controller-0",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			id := createResponse.GetObject().GetId()

			response, err := leasesServer.List(ctx, privatev1.LeasesListRequest_builder{
				Filter: proto.String(fmt.Sprintf("this.id == '%s'", id)),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
			Expect(response.GetItems()[0].GetId()).To(Equal(id))
		})

		It("Gets a lease", func() {
			createResponse, err := leasesServer.Create(ctx, privatev1.LeasesCreateRequest_builder{
				Object: privatev1.Lease_builder{
					Spec: privatev1.LeaseSpec_builder{
						Holder:   "controller-0",
						Duration: durationpb.New(15_000_000_000),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := leasesServer.Get(ctx, privatev1.LeasesGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(createResponse.GetObject(), getResponse.GetObject())).To(BeTrue())
		})

		It("Updates a lease", func() {
			createResponse, err := leasesServer.Create(ctx, privatev1.LeasesCreateRequest_builder{
				Object: privatev1.Lease_builder{
					Spec: privatev1.LeaseSpec_builder{
						Holder:           "controller-0",
						Duration:         durationpb.New(15_000_000_000),
						AcquireTimestamp: timestamppb.Now(),
						RenewTimestamp:   timestamppb.Now(),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			newRenewTime := timestamppb.Now()
			updateResponse, err := leasesServer.Update(ctx, privatev1.LeasesUpdateRequest_builder{
				Object: privatev1.Lease_builder{
					Id: object.GetId(),
					Spec: privatev1.LeaseSpec_builder{
						RenewTimestamp: newRenewTime,
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"spec.renew_timestamp",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(updateResponse.GetObject().GetSpec().GetRenewTimestamp(), newRenewTime)).To(BeTrue())
		})

		It("Deletes a lease", func() {
			createResponse, err := leasesServer.Create(ctx, privatev1.LeasesCreateRequest_builder{
				Object: privatev1.Lease_builder{
					Metadata: privatev1.Metadata_builder{
						Finalizers: []string{"test"},
					}.Build(),
					Spec: privatev1.LeaseSpec_builder{
						Holder: "controller-0",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			_, err = leasesServer.Delete(ctx, privatev1.LeasesDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := leasesServer.Get(ctx, privatev1.LeasesGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})
	})
})
