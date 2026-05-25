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
	"math"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
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

			// Set pool to READY with capacity (required by PublicIP Create validation):
			_, err = poolsServer.Update(ctx, privatev1.PublicIPPoolsUpdateRequest_builder{
				Object: privatev1.PublicIPPool_builder{
					Id: poolID,
					Status: privatev1.PublicIPPoolStatus_builder{
						State:     privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY,
						Total:     10,
						Allocated: 0,
						Available: 10,
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"status"},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())

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

	Describe("Validation tests", func() {
		var poolsServer *PrivatePublicIPPoolsServer

		BeforeEach(func() {
			var err error

			poolsServer, err = NewPrivatePublicIPPoolsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		Context("POOL-VAL-01: ip_family validation", func() {
			It("rejects pool with unspecified ip_family", func() {
				pool := privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs: []string{"192.168.1.0/24"},
					}.Build(),
				}.Build()

				err := poolsServer.validateCreate(ctx, pool)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("ip_family"))
			})
		})

		Context("POOL-VAL-02: CIDRs required", func() {
			It("rejects pool with no CIDRs", func() {
				pool := privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build()

				err := poolsServer.validateCreate(ctx, pool)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("cidrs"))
			})
		})

		Context("CIDR format validation", func() {
			It("rejects malformed CIDR", func() {
				pool := privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"not-a-cidr"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build()

				err := poolsServer.validateCreate(ctx, pool)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("invalid CIDR format"))
			})

			It("reports the correct index in the error message for an invalid CIDR", func() {
				pool := privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"10.0.0.0/24", "bad-cidr"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build()

				err := poolsServer.validateCreate(ctx, pool)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("cidrs[1]"))
			})
		})

		Context("IP family consistency", func() {
			It("rejects IPv6 CIDR in an IPv4 pool", func() {
				pool := privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"2001:db8::/64"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build()

				err := poolsServer.validateCreate(ctx, pool)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("IPv6"))
				Expect(err.Error()).To(ContainSubstring("ip_family is IPv4"))
			})

			It("rejects IPv4 CIDR in an IPv6 pool", func() {
				pool := privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"10.0.0.0/24"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV6,
					}.Build(),
				}.Build()

				err := poolsServer.validateCreate(ctx, pool)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("IPv4"))
				Expect(err.Error()).To(ContainSubstring("ip_family is IPv6"))
			})

			It("rejects a pool with mixed IPv4 and IPv6 CIDRs (ip_family=IPv4)", func() {
				pool := privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"10.0.0.0/24", "2001:db8::/64"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build()

				err := poolsServer.validateCreate(ctx, pool)
				Expect(err).To(HaveOccurred())
				// The IPv6 CIDR at index 1 fails the IPv4 family check.
				Expect(err.Error()).To(ContainSubstring("cidrs[1]"))
				Expect(err.Error()).To(ContainSubstring("ip_family is IPv4"))
			})

			It("accepts multiple IPv4 CIDRs in an IPv4 pool", func() {
				pool := privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"10.0.0.0/24", "192.168.1.0/28"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build()

				err := poolsServer.validateCreate(ctx, pool)
				Expect(err).ToNot(HaveOccurred())
			})

		})

		Context("Cross-pool CIDR overlap detection", func() {
			It("rejects a pool whose CIDR exactly matches an existing pool", func() {
				_, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Metadata: privatev1.Metadata_builder{Name: "pool-a"}.Build(),
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				_, err = poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Metadata: privatev1.Metadata_builder{Name: "pool-b"}.Build(),
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.AlreadyExists))
				Expect(err.Error()).To(ContainSubstring("overlaps"))
				Expect(err.Error()).To(ContainSubstring("pool-a"))
			})

			It("rejects a pool whose CIDR is a subset of an existing pool's CIDR", func() {
				_, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Metadata: privatev1.Metadata_builder{Name: "wide-pool"}.Build(),
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/16"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				_, err = poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.1.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.AlreadyExists))
			})

			It("rejects a pool whose CIDR is a superset of an existing pool's CIDR", func() {
				_, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.1.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				_, err = poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/16"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.AlreadyExists))
			})

			It("accepts a pool with a non-overlapping CIDR", func() {
				_, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				_, err = poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.1.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			})

			It("rejects when one of multiple new CIDRs overlaps with an existing pool", func() {
				_, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Metadata: privatev1.Metadata_builder{Name: "first-pool"}.Build(),
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				_, err = poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs: []string{
								"192.168.0.0/24", // safe
								"10.0.0.0/24",    // overlaps first-pool
							},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.AlreadyExists))
				Expect(err.Error()).To(ContainSubstring("overlaps"))
			})
		})

		Context("Intra-pool CIDR overlap detection", func() {
			It("rejects a pool whose second CIDR is a subset of the first", func() {
				pool := privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"10.0.0.0/24", "10.0.0.0/25"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build()

				err := poolsServer.validateCreate(ctx, pool)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("cidrs[0]"))
				Expect(err.Error()).To(ContainSubstring("cidrs[1]"))
				Expect(err.Error()).To(ContainSubstring("overlaps"))
			})

			It("accepts a pool with non-overlapping CIDRs", func() {
				pool := privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
					}.Build(),
				}.Build()

				err := poolsServer.validateCreate(ctx, pool)
				Expect(err).ToNot(HaveOccurred())
			})

			It("rejects an IPv6 pool with overlapping CIDRs", func() {
				pool := privatev1.PublicIPPool_builder{
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs:    []string{"2001:db8::/32", "2001:db8::/64"},
						IpFamily: privatev1.IPFamily_IP_FAMILY_IPV6,
					}.Build(),
				}.Build()

				err := poolsServer.validateCreate(ctx, pool)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("overlaps"))
			})
		})

		Context("Capacity calculation on Create", func() {
			It("sets status.total to 254 for a /24 IPv4 CIDR", func() {
				response, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"192.168.1.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetObject().GetStatus().GetTotal()).To(BeNumerically("==", 254))
			})

			It("sets status.available equal to status.total on a new pool (nothing allocated yet)", func() {
				response, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				status := response.GetObject().GetStatus()
				Expect(status.GetAvailable()).To(Equal(status.GetTotal()))
				Expect(status.GetAllocated()).To(BeNumerically("==", 0))
			})

			It("sets status.total to 14 for a /28 IPv4 CIDR", func() {
				response, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/28"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetObject().GetStatus().GetTotal()).To(BeNumerically("==", 14))
			})

			It("sets status.total to 2 for a /30 IPv4 CIDR", func() {
				response, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/30"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetObject().GetStatus().GetTotal()).To(BeNumerically("==", 2))
			})

			It("sets status.total to 2 for a /31 IPv4 CIDR (point-to-point)", func() {
				response, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/31"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetObject().GetStatus().GetTotal()).To(BeNumerically("==", 2))
			})

			It("sets status.total to 1 for a /32 IPv4 CIDR (host route)", func() {
				response, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.1/32"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetObject().GetStatus().GetTotal()).To(BeNumerically("==", 1))
			})

			It("sums status.total across multiple CIDRs", func() {
				// /24 → 254, /28 → 14: total = 268
				response, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/24", "10.1.0.0/28"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetObject().GetStatus().GetTotal()).To(BeNumerically("==", 268))
			})

			It("sets correct status.total for an IPv6 /64 CIDR", func() {
				response, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"2001:db8::/64"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV6,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				// /64 has 2^64 addresses; capped at math.MaxInt64.
				Expect(response.GetObject().GetStatus().GetTotal()).To(BeNumerically("==", math.MaxInt64))
			})

			It("sets status.total to 1 for an IPv6 /128 CIDR", func() {
				response, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"2001:db8::1/128"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV6,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetObject().GetStatus().GetTotal()).To(BeNumerically("==", 1))
			})
		})

		Context("Immutability on Update", func() {
			It("prevents changing spec.cidrs after creation", func() {
				createResponse, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				id := createResponse.GetObject().GetId()

				_, err = poolsServer.Update(ctx, privatev1.PublicIPPoolsUpdateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Id: id,
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.1.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("cidrs"))
				Expect(err.Error()).To(ContainSubstring("immutable"))
			})

			It("prevents changing spec.ip_family after creation", func() {
				createResponse, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				id := createResponse.GetObject().GetId()

				_, err = poolsServer.Update(ctx, privatev1.PublicIPPoolsUpdateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Id: id,
						Spec: privatev1.PublicIPPoolSpec_builder{
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV6,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("ip_family"))
				Expect(err.Error()).To(ContainSubstring("immutable"))
			})

			It("allows providing identical CIDRs on Update without error", func() {
				createResponse, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				id := createResponse.GetObject().GetId()

				_, err = poolsServer.Update(ctx, privatev1.PublicIPPoolsUpdateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Id: id,
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
						Metadata: privatev1.Metadata_builder{
							Name: "renamed",
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			})

			It("allows metadata-only Update (spec fields omitted)", func() {
				createResponse, err := poolsServer.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Spec: privatev1.PublicIPPoolSpec_builder{
							Cidrs:    []string{"10.0.0.0/24"},
							IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				id := createResponse.GetObject().GetId()

				updateResponse, err := poolsServer.Update(ctx, privatev1.PublicIPPoolsUpdateRequest_builder{
					Object: privatev1.PublicIPPool_builder{
						Id: id,
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
				// CIDRs must be preserved:
				Expect(updateResponse.GetObject().GetSpec().GetCidrs()).To(ConsistOf("10.0.0.0/24"))
			})
		})

		Context("Unit tests for capacity calculation helpers", func() {
			DescribeTable("calculateCIDRCapacity",
				func(cidr string, family privatev1.IPFamily, expected int64) {
					Expect(calculateCIDRCapacity(cidr, family)).To(BeNumerically("==", expected))
				},
				Entry("IPv4 /24 → 254", "192.168.1.0/24", privatev1.IPFamily_IP_FAMILY_IPV4, int64(254)),
				Entry("IPv4 /28 → 14", "10.0.0.0/28", privatev1.IPFamily_IP_FAMILY_IPV4, int64(14)),
				Entry("IPv4 /30 → 2", "10.0.0.0/30", privatev1.IPFamily_IP_FAMILY_IPV4, int64(2)),
				Entry("IPv4 /31 → 2", "10.0.0.0/31", privatev1.IPFamily_IP_FAMILY_IPV4, int64(2)),
				Entry("IPv4 /32 → 1", "10.0.0.1/32", privatev1.IPFamily_IP_FAMILY_IPV4, int64(1)),
				Entry("IPv4 /16 → 65534", "10.0.0.0/16", privatev1.IPFamily_IP_FAMILY_IPV4, int64(65534)),
				Entry("IPv6 /128 → 1", "2001:db8::1/128", privatev1.IPFamily_IP_FAMILY_IPV6, int64(1)),
				Entry("IPv6 /127 → 2", "2001:db8::/127", privatev1.IPFamily_IP_FAMILY_IPV6, int64(2)),
				Entry("IPv6 /64 → MaxInt64", "2001:db8::/64", privatev1.IPFamily_IP_FAMILY_IPV6, int64(math.MaxInt64)),
			)

			It("calculateCIDRCapacity returns 0 for an unparseable CIDR", func() {
				result := calculateCIDRCapacity("not-a-cidr", privatev1.IPFamily_IP_FAMILY_IPV4)
				Expect(result).To(BeNumerically("==", 0))
			})

		})
	})
})
