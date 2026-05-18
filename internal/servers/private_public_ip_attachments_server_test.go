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

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Private public IP attachments server", func() {
	var (
		ctx                       context.Context
		tx                        database.Tx
		publicIPAttachmentsServer *PrivatePublicIPAttachmentsServer
	)

	BeforeEach(func() {
		var err error

		ctx = context.Background()

		db := server.MakeDatabase()
		DeferCleanup(db.Close)
		pool, err := pgxpool.New(ctx, db.MakeURL())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)

		tm, err := database.NewTxManager().
			SetLogger(logger).
			SetPool(pool).
			Build()
		Expect(err).ToNot(HaveOccurred())

		tx, err = tm.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := tm.End(ctx, tx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)

		err = dao.CreateTables[*privatev1.PublicIPAttachment](ctx)
		Expect(err).ToNot(HaveOccurred())

		publicIPAttachmentsServer, err = NewPrivatePublicIPAttachmentsServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all required parameters are set", func() {
			server, err := NewPrivatePublicIPAttachmentsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivatePublicIPAttachmentsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivatePublicIPAttachmentsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		It("Creates a public IP attachment", func() {
			response, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        "019728a4-3f5c-7def-8abc-1234567890ab",
						ComputeInstance: proto.String("01972f1b-a4e9-7c82-9def-abcdef123456"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.GetObject()).ToNot(BeNil())
			Expect(response.GetObject().GetId()).ToNot(BeEmpty())
			Expect(response.GetObject().GetMetadata().GetName()).To(Equal("my-attachment"))
			Expect(response.GetObject().GetSpec().GetPublicIp()).To(Equal("019728a4-3f5c-7def-8abc-1234567890ab"))
			Expect(response.GetObject().GetSpec().GetComputeInstance()).To(Equal("01972f1b-a4e9-7c82-9def-abcdef123456"))
		})

		It("Lists public IP attachments", func() {
			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "attachment-a",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        "ip-a",
						ComputeInstance: proto.String("ci-a"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "attachment-b",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        "ip-b",
						ComputeInstance: proto.String("ci-b"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			listResponse, err := publicIPAttachmentsServer.List(ctx, privatev1.PublicIPAttachmentsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(listResponse.GetSize()).To(Equal(int32(2)))
			Expect(listResponse.GetItems()).To(HaveLen(2))
		})

		It("Gets a public IP attachment", func() {
			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        "ip-1",
						ComputeInstance: proto.String("ci-1"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := publicIPAttachmentsServer.Get(ctx, privatev1.PublicIPAttachmentsGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetId()).To(Equal(createResponse.GetObject().GetId()))
			Expect(getResponse.GetObject().GetMetadata().GetName()).To(Equal("my-attachment"))
			Expect(getResponse.GetObject().GetSpec().GetPublicIp()).To(Equal("ip-1"))
			Expect(getResponse.GetObject().GetSpec().GetComputeInstance()).To(Equal("ci-1"))
		})

		It("Updates a public IP attachment", func() {
			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        "ip-original",
						ComputeInstance: proto.String("ci-original"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			updateResponse, err := publicIPAttachmentsServer.Update(ctx, privatev1.PublicIPAttachmentsUpdateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Id: createResponse.GetObject().GetId(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp: "ip-updated",
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"spec.public_ip",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetSpec().GetPublicIp()).To(Equal("ip-updated"))
		})

		It("Ignores fields not included in the update mask", func() {
			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        "ip-original",
						ComputeInstance: proto.String("ci-original"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			updateResponse, err := publicIPAttachmentsServer.Update(ctx, privatev1.PublicIPAttachmentsUpdateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Id: createResponse.GetObject().GetId(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        "ip-updated",
						ComputeInstance: proto.String("ci-updated"),
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"spec.public_ip",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetSpec().GetPublicIp()).To(Equal("ip-updated"))
			Expect(updateResponse.GetObject().GetSpec().GetComputeInstance()).To(Equal("ci-original"))
		})

		It("Deletes a public IP attachment", func() {
			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        "ip-1",
						ComputeInstance: proto.String("ci-1"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = publicIPAttachmentsServer.Delete(ctx, privatev1.PublicIPAttachmentsDeleteRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Signals a public IP attachment", func() {
			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        "ip-1",
						ComputeInstance: proto.String("ci-1"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = publicIPAttachmentsServer.Signal(ctx, privatev1.PublicIPAttachmentsSignalRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
