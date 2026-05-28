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
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

func createPublicIPInState(
	ctx context.Context,
	publicIPDao *dao.GenericDAO[*privatev1.PublicIP],
	poolID string,
	state privatev1.PublicIPState,
	attached bool,
) *privatev1.PublicIP {
	resp, err := publicIPDao.Create().SetObject(
		privatev1.PublicIP_builder{
			Metadata: privatev1.Metadata_builder{
				Tenant: "shared",
			}.Build(),
			Spec: privatev1.PublicIPSpec_builder{
				Pool: poolID,
			}.Build(),
			Status: privatev1.PublicIPStatus_builder{
				State:    state,
				Address:  "10.0.0.1",
				Attached: attached,
			}.Build(),
		}.Build(),
	).Do(ctx)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	return resp.GetObject()
}

func createComputeInstanceInState(
	ctx context.Context,
	computeInstanceDao *dao.GenericDAO[*privatev1.ComputeInstance],
	state privatev1.ComputeInstanceState,
) *privatev1.ComputeInstance {
	resp, err := computeInstanceDao.Create().SetObject(
		privatev1.ComputeInstance_builder{
			Metadata: privatev1.Metadata_builder{
				Tenant: "shared",
			}.Build(),
			Spec: privatev1.ComputeInstanceSpec_builder{
				Template: "general.small",
			}.Build(),
			Status: privatev1.ComputeInstanceStatus_builder{
				State: state,
			}.Build(),
		}.Build(),
	).Do(ctx)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	return resp.GetObject()
}

var _ = Describe("Public IP attachments server", func() {
	var (
		ctx context.Context
		tx  database.Tx
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
			err := tm.End(ctx, tx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPublicIPAttachmentsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPublicIPAttachmentsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPublicIPAttachmentsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPublicIPAttachmentsServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var (
			publicIPAttachmentsServer *PublicIPAttachmentsServer
			publicIPDao               *dao.GenericDAO[*privatev1.PublicIP]
			computeInstanceDao        *dao.GenericDAO[*privatev1.ComputeInstance]
			sharedPoolID              string
		)

		BeforeEach(func() {
			var err error

			publicIPDao, err = dao.NewGenericDAO[*privatev1.PublicIP]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			computeInstanceDao, err = dao.NewGenericDAO[*privatev1.ComputeInstance]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			publicIPPoolDao, err := dao.NewGenericDAO[*privatev1.PublicIPPool]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			poolResp, err := publicIPPoolDao.Create().SetObject(
				privatev1.PublicIPPool_builder{
					Metadata: privatev1.Metadata_builder{
						Tenant: "shared",
					}.Build(),
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs: []string{"10.0.0.0/24"},
					}.Build(),
					Status: privatev1.PublicIPPoolStatus_builder{
						State:     privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY,
						Total:     100,
						Allocated: 0,
						Available: 100,
					}.Build(),
				}.Build(),
			).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			sharedPoolID = poolResp.GetObject().GetId()

			publicIPAttachmentsServer, err = NewPublicIPAttachmentsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		createAttachment := func() *publicv1.PublicIPAttachment {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPoolID, privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			response, err := publicIPAttachmentsServer.Create(ctx, publicv1.PublicIPAttachmentsCreateRequest_builder{
				Object: publicv1.PublicIPAttachment_builder{
					Spec: publicv1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			return response.GetObject()
		}

		It("Creates object", func() {
			object := createAttachment()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())
			Expect(object.GetSpec().GetPublicIp()).ToNot(BeEmpty())
			Expect(object.GetSpec().GetComputeInstance()).ToNot(BeEmpty())
		})

		It("List objects", func() {
			const count = 10
			for range count {
				createAttachment()
			}

			response, err := publicIPAttachmentsServer.List(ctx, publicv1.PublicIPAttachmentsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			const count = 10
			for range count {
				createAttachment()
			}

			response, err := publicIPAttachmentsServer.List(ctx, publicv1.PublicIPAttachmentsListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			const count = 10
			for range count {
				createAttachment()
			}

			response, err := publicIPAttachmentsServer.List(ctx, publicv1.PublicIPAttachmentsListRequest_builder{
				Offset: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-1))
		})

		It("List objects with filter", func() {
			const count = 10
			var objects []*publicv1.PublicIPAttachment
			for range count {
				objects = append(objects, createAttachment())
			}

			for _, object := range objects {
				response, err := publicIPAttachmentsServer.List(ctx, publicv1.PublicIPAttachmentsListRequest_builder{
					Filter: new(fmt.Sprintf("this.id == '%s'", object.GetId())),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 1))
				Expect(response.GetItems()[0].GetId()).To(Equal(object.GetId()))
			}
		})

		It("Get object", func() {
			created := createAttachment()

			getResponse, err := publicIPAttachmentsServer.Get(ctx, publicv1.PublicIPAttachmentsGetRequest_builder{
				Id: created.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(created, getResponse.GetObject())).To(BeTrue())
		})

		It("Delete object", func() {
			created := createAttachment()

			// Add a finalizer and transition to READY. Finalizer prevents immediate
			// archival so we can verify the deletion timestamp. State must be READY
			// because only non-PENDING PublicIPAttachments can be deleted. Both are
			// set via raw SQL because the public API doesn't expose finalizers or
			// status.state.
			_, err := tx.Exec(
				ctx,
				`update public_ip_attachments set finalizers = '{"a"}',`+
					` data = jsonb_set(data, '{status,state}', '"PUBLIC_IP_ATTACHMENT_STATE_READY"') where id = $1`,
				created.GetId(),
			)
			Expect(err).ToNot(HaveOccurred())

			// Delete the object:
			_, err = publicIPAttachmentsServer.Delete(ctx, publicv1.PublicIPAttachmentsDeleteRequest_builder{
				Id: created.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get and verify:
			getResponse, err := publicIPAttachmentsServer.Get(ctx, publicv1.PublicIPAttachmentsGetRequest_builder{
				Id: created.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := getResponse.GetObject()
			Expect(object.GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})
	})
})
