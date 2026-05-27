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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Private public IP attachments server", func() {
	var (
		ctx                       context.Context
		tx                        database.Tx
		publicIPAttachmentsServer *PrivatePublicIPAttachmentsServer
		publicIPPoolDao           *dao.GenericDAO[*privatev1.PublicIPPool]
		publicIPDao               *dao.GenericDAO[*privatev1.PublicIP]
		computeInstanceDao        *dao.GenericDAO[*privatev1.ComputeInstance]
		sharedPool                *privatev1.PublicIPPool
	)

	BeforeEach(func() {
		var err error

		ctx = context.Background()

		db, err := server.NewInstance().Build()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(db.Close)
		pool, err := db.Pool(ctx)
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

		// Create the tenants used in the tests:
		tenantsDao, err := dao.NewGenericDAO[*privatev1.Organization]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		createTenant := func(name string) {
			_, err = tenantsDao.Create().
				SetObject(privatev1.Organization_builder{
					Id: name,
					Metadata: privatev1.Metadata_builder{
						Name:   name,
						Tenant: name,
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
		}
		createTenant("other-tenant")

		publicIPPoolDao, err = dao.NewGenericDAO[*privatev1.PublicIPPool]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

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

		poolResp, err := publicIPPoolDao.Create().SetObject(
			privatev1.PublicIPPool_builder{
				Metadata: privatev1.Metadata_builder{
					Tenant: auth.SharedTenant,
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
		sharedPool = poolResp.GetObject()

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
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			response, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.GetObject()).ToNot(BeNil())
			Expect(response.GetObject().GetId()).ToNot(BeEmpty())
			Expect(response.GetObject().GetMetadata().GetName()).To(Equal("my-attachment"))
			Expect(response.GetObject().GetSpec().GetPublicIp()).To(Equal(pip.GetId()))
			Expect(response.GetObject().GetSpec().GetComputeInstance()).To(Equal(ci.GetId()))
		})

		It("Lists public IP attachments", func() {
			pip1 := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci1 := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)
			pip2 := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci2 := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "attachment-a",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip1.GetId(),
						ComputeInstance: new(ci1.GetId()),
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
						PublicIp:        pip2.GetId(),
						ComputeInstance: new(ci2.GetId()),
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
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
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
			Expect(getResponse.GetObject().GetSpec().GetPublicIp()).To(Equal(pip.GetId()))
			Expect(getResponse.GetObject().GetSpec().GetComputeInstance()).To(Equal(ci.GetId()))
		})

		It("Updates metadata of a public IP attachment", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			updateResponse, err := publicIPAttachmentsServer.Update(ctx, privatev1.PublicIPAttachmentsUpdateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Id: createResponse.GetObject().GetId(),
					Metadata: privatev1.Metadata_builder{
						Name: "renamed-attachment",
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"metadata.name",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetMetadata().GetName()).To(Equal("renamed-attachment"))
			Expect(updateResponse.GetObject().GetSpec().GetPublicIp()).To(Equal(pip.GetId()))
			Expect(updateResponse.GetObject().GetSpec().GetComputeInstance()).To(Equal(ci.GetId()))
		})

		It("Rejects update of spec.public_ip", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			pip2 := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			_, err = publicIPAttachmentsServer.Update(ctx, privatev1.PublicIPAttachmentsUpdateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Id: createResponse.GetObject().GetId(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp: pip2.GetId(),
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"spec.public_ip",
					},
				},
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("spec.public_ip"))
			Expect(err.Error()).To(ContainSubstring("immutable"))
		})

		It("Rejects update of spec.compute_instance", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			ci2 := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)
			_, err = publicIPAttachmentsServer.Update(ctx, privatev1.PublicIPAttachmentsUpdateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Id: createResponse.GetObject().GetId(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci2.GetId()),
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"spec.compute_instance",
					},
				},
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("spec.compute_instance"))
			Expect(err.Error()).To(ContainSubstring("immutable"))
		})

		It("Allows update when spec fields are unchanged", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			updateResponse, err := publicIPAttachmentsServer.Update(ctx, privatev1.PublicIPAttachmentsUpdateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Id: createResponse.GetObject().GetId(),
					Metadata: privatev1.Metadata_builder{
						Name: "renamed",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"metadata.name",
						"spec.public_ip",
						"spec.compute_instance",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetMetadata().GetName()).To(Equal("renamed"))
			Expect(updateResponse.GetObject().GetSpec().GetPublicIp()).To(Equal(pip.GetId()))
			Expect(updateResponse.GetObject().GetSpec().GetComputeInstance()).To(Equal(ci.GetId()))
		})

		It("Rejects full object replacement with changed spec.public_ip (nil mask)", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			pip2 := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			_, err = publicIPAttachmentsServer.Update(ctx, privatev1.PublicIPAttachmentsUpdateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Id: createResponse.GetObject().GetId(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip2.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("immutable"))
		})

		It("Deletes a public IP attachment", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
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
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			createResponse, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
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

	Describe("Validation", func() {
		It("Rejects Create with nil object", func() {
			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("is mandatory"))
		})

		It("Rejects Create with nil spec", func() {
			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-attachment",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("spec is mandatory"))
		})

		It("Rejects Create with empty spec.public_ip", func() {
			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						ComputeInstance: new("some-ci"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("spec.public_ip"))
		})

		It("Rejects Create with missing ComputeInstance", func() {
			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp: "some-ip",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("spec.compute_instance"))
		})
	})

	Describe("PublicIP reference validation", func() {
		It("Rejects Create when PublicIP does not exist", func() {
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        "nonexistent-public-ip-id",
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})

		It("Rejects Create when PublicIP is not in ALLOCATED state", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(err.Error()).To(ContainSubstring("not in ALLOCATED state"))
		})

	})

	Describe("ComputeInstance reference validation", func() {
		It("Rejects Create when ComputeInstance does not exist", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)

			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new("nonexistent-ci-id"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})

		It("Rejects Create when ComputeInstance is not in RUNNING state", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STOPPED)

			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(err.Error()).To(ContainSubstring("not in RUNNING state"))
		})
	})

	Describe("Cross-tenant validation", func() {
		It("Rejects Create when PublicIP belongs to a different tenant", func() {
			otherTenantCtrl := gomock.NewController(GinkgoT())
			DeferCleanup(otherTenantCtrl.Finish)

			restrictedTenancy := auth.NewMockTenancyLogic(otherTenantCtrl)
			restrictedTenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
				Return(collections.NewSet("shared"), nil).
				AnyTimes()
			restrictedTenancy.EXPECT().DetermineDefaultTenant(gomock.Any()).
				Return("shared", nil).
				AnyTimes()
			restrictedTenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
				Return(collections.NewSet("shared"), nil).
				AnyTimes()

			restrictedServer, err := NewPrivatePublicIPAttachmentsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(restrictedTenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			otherTenantIP, err := publicIPDao.Create().SetObject(
				privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenant: "other-tenant",
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: sharedPool.GetId(),
					}.Build(),
					Status: privatev1.PublicIPStatus_builder{
						State:    privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED,
						Address:  "10.0.0.99",
						Attached: false,
					}.Build(),
				}.Build(),
			).Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			_, err = restrictedServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        otherTenantIP.GetObject().GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})

		It("Rejects Create when ComputeInstance belongs to a different tenant", func() {
			otherTenantCtrl := gomock.NewController(GinkgoT())
			DeferCleanup(otherTenantCtrl.Finish)

			restrictedTenancy := auth.NewMockTenancyLogic(otherTenantCtrl)
			restrictedTenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
				Return(collections.NewSet("shared"), nil).
				AnyTimes()
			restrictedTenancy.EXPECT().DetermineDefaultTenant(gomock.Any()).
				Return("shared", nil).
				AnyTimes()
			restrictedTenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
				Return(collections.NewSet("shared"), nil).
				AnyTimes()

			restrictedServer, err := NewPrivatePublicIPAttachmentsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(restrictedTenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)

			otherTenantCI, err := computeInstanceDao.Create().SetObject(
				privatev1.ComputeInstance_builder{
					Metadata: privatev1.Metadata_builder{
						Tenant: "other-tenant",
					}.Build(),
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: "general.small",
					}.Build(),
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
					}.Build(),
				}.Build(),
			).Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			_, err = restrictedServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(otherTenantCI.GetObject().GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})
	})

	Describe("Uniqueness constraints", func() {
		It("Rejects Create when PublicIP already has an attachment", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci1 := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)
			ci2 := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci1.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci2.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.AlreadyExists))
			Expect(err.Error()).To(ContainSubstring("PublicIP"))
		})

		It("Rejects Create when ComputeInstance already has an attachment", func() {
			pip1 := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			pip2 := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip1.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip2.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.AlreadyExists))
			Expect(err.Error()).To(ContainSubstring("ComputeInstance"))
		})
	})

	Describe("Attached flag management", func() {
		It("Sets PublicIP.status.attached to true on Create", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			_, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			ipResp, err := publicIPDao.Get().SetId(pip.GetId()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(ipResp.GetObject().GetStatus().GetAttached()).To(BeTrue())
		})

		It("Sets PublicIP.status.attached to false on Delete", func() {
			pip := createPublicIPInState(ctx, publicIPDao, sharedPool.GetId(), privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED, false)
			ci := createComputeInstanceInState(ctx, computeInstanceDao, privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING)

			createResp, err := publicIPAttachmentsServer.Create(ctx, privatev1.PublicIPAttachmentsCreateRequest_builder{
				Object: privatev1.PublicIPAttachment_builder{
					Spec: privatev1.PublicIPAttachmentSpec_builder{
						PublicIp:        pip.GetId(),
						ComputeInstance: new(ci.GetId()),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = publicIPAttachmentsServer.Delete(ctx, privatev1.PublicIPAttachmentsDeleteRequest_builder{
				Id: createResp.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			ipResp, err := publicIPDao.Get().SetId(pip.GetId()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(ipResp.GetObject().GetStatus().GetAttached()).To(BeFalse())
		})
	})

	Describe("Delete behaviour", func() {
		It("Returns error for empty ID on Delete", func() {
			_, err := publicIPAttachmentsServer.Delete(ctx, privatev1.PublicIPAttachmentsDeleteRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("identifier is mandatory"))
		})
	})
})
