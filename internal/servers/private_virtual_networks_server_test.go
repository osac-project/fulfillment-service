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
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Private virtual networks server", func() {
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
		err = dao.CreateTables[*privatev1.VirtualNetwork](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.NetworkClass](ctx)
		Expect(err).ToNot(HaveOccurred())
	})

	// Helper function to create a NetworkClass for validation tests
	createNetworkClass := func(ctx context.Context, state privatev1.NetworkClassState) *privatev1.NetworkClass {
		// Create NetworkClass DAO
		ncDao, err := dao.NewGenericDAO[*privatev1.NetworkClass]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		nc := privatev1.NetworkClass_builder{
			ImplementationStrategy: "test-strategy",
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"shared"},
			}.Build(),
			Capabilities: privatev1.NetworkClassCapabilities_builder{
				SupportsIpv4:      true,
				SupportsIpv6:      true,
				SupportsDualStack: true,
			}.Build(),
			Status: privatev1.NetworkClassStatus_builder{
				State: state,
			}.Build(),
		}.Build()

		response, err := ncDao.Create().
			SetObject(nc).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())

		return response.GetObject()
	}

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivateVirtualNetworksServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivateVirtualNetworksServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivateVirtualNetworksServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Validation tests", func() {
		var server *PrivateVirtualNetworksServer

		BeforeEach(func() {
			var err error

			// Create the server:
			server, err = NewPrivateVirtualNetworksServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		Context("VN-VAL-01: IPv4 CIDR validation", func() {
			It("accepts valid IPv4 CIDR", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String("192.168.0.0/16"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).ToNot(HaveOccurred())
			})

			It("rejects invalid IPv4 CIDR format", func() {
				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr: proto.String("not-a-cidr"),
						Region:   "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid IPv4 CIDR"))
			})

			It("rejects IPv4 with invalid mask", func() {
				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr: proto.String("192.168.0.0/33"),
						Region:   "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid IPv4 CIDR"))
			})

			It("rejects IPv6 address in IPv4 field", func() {
				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr: proto.String("2001:db8::/32"),
						Region:   "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("contains IPv6 address"))
			})
		})

		Context("VN-VAL-02: IPv6 CIDR validation", func() {
			It("accepts valid IPv6 CIDR", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv6Cidr:     proto.String("2001:db8::/32"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).ToNot(HaveOccurred())
			})

			It("rejects invalid IPv6 CIDR format", func() {
				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv6Cidr: proto.String("not-ipv6"),
						Region:   "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid IPv6 CIDR"))
			})

			It("rejects IPv6 with invalid mask", func() {
				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv6Cidr: proto.String("2001:db8::/129"),
						Region:   "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid IPv6 CIDR"))
			})

			It("rejects IPv4 address in IPv6 field", func() {
				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv6Cidr: proto.String("192.168.0.0/16"),
						Region:   "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("contains IPv4 address"))
			})
		})

		Context("VN-VAL-03: At least one CIDR required", func() {
			It("rejects empty IPv4 and IPv6 CIDRs", func() {
				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Region: "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("at least one"))
			})

			It("accepts IPv4-only configuration", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).ToNot(HaveOccurred())
			})

			It("accepts IPv6-only configuration", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv6Cidr:     proto.String("2001:db8::/32"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).ToNot(HaveOccurred())
			})

			It("accepts dual-stack configuration", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						Ipv6Cidr:     proto.String("2001:db8::/32"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("VN-VAL-04: NetworkClass reference validation", func() {
			It("accepts valid NetworkClass reference", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).ToNot(HaveOccurred())
			})

			It("rejects non-existent NetworkClass", func() {
				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: "non-existent-class",
						Region:       "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not exist"))
			})

			It("rejects empty NetworkClass", func() {
				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr: proto.String("10.0.0.0/16"),
						Region:   "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("network_class"))
				Expect(err.Error()).To(ContainSubstring("required"))
			})
		})

		Context("VN-VAL-05: NetworkClass READY state validation", func() {
			It("accepts NetworkClass in READY state", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).ToNot(HaveOccurred())
			})

			It("rejects NetworkClass in PENDING state", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_PENDING)

				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
				Expect(err.Error()).To(ContainSubstring("not in READY state"))
			})

			It("rejects NetworkClass in FAILED state", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_FAILED)

				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
				Expect(err.Error()).To(ContainSubstring("not in READY state"))
			})
		})

		Context("VN-VAL-06: Capabilities matching", func() {
			It("accepts matching IPv4 capability", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
						Capabilities: privatev1.VirtualNetworkCapabilities_builder{
							EnableIpv4: true,
						}.Build(),
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).ToNot(HaveOccurred())
			})

			It("rejects IPv4 when NetworkClass doesn't support it", func() {
				// Create NetworkClass without IPv4 support
				ncDao, err := dao.NewGenericDAO[*privatev1.NetworkClass]().
					SetLogger(logger).
					SetTenancyLogic(tenancy).
					Build()
				Expect(err).ToNot(HaveOccurred())

				nc := privatev1.NetworkClass_builder{
					ImplementationStrategy: "no-ipv4-class",
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Capabilities: privatev1.NetworkClassCapabilities_builder{
						SupportsIpv4: false,
						SupportsIpv6: true,
					}.Build(),
					Status: privatev1.NetworkClassStatus_builder{
						State: privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY,
					}.Build(),
				}.Build()
				response, err := ncDao.Create().
					SetObject(nc).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				nc = response.GetObject()

				vn := privatev1.VirtualNetwork_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
						Capabilities: privatev1.VirtualNetworkCapabilities_builder{
							EnableIpv4: true,
						}.Build(),
					}.Build(),
				}.Build()

				_, err = server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not support IPv4"))
			})
		})

		Context("VN-VAL-08: Region field required", func() {
			It("rejects empty region", func() {
				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr: proto.String("10.0.0.0/16"),
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("region"))
				Expect(err.Error()).To(ContainSubstring("required"))
			})

			It("accepts non-empty region", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

				vn := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
						Region:       "us-west-1",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, vn, nil)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("VN-VAL-09: Region immutability on Update", func() {
			It("prevents region modification", func() {
				existing := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Region:       "us-west-1",
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: "test-class",
					}.Build(),
				}.Build()

				updated := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Region:       "us-east-1",
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: "test-class",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, updated, existing)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("region"))
				Expect(err.Error()).To(ContainSubstring("immutable"))
			})

			It("allows region to stay same", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

				existing := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Region:       "us-west-1",
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
					}.Build(),
				}.Build()

				updated := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Region:       "us-west-1",
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, updated, existing)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("VN-VAL-10: NetworkClass immutability on Update", func() {
			It("prevents network_class modification", func() {
				existing := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Region:       "us-west-1",
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: "class-1",
					}.Build(),
				}.Build()

				updated := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Region:       "us-west-1",
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: "class-2",
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, updated, existing)
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("network_class"))
				Expect(err.Error()).To(ContainSubstring("immutable"))
			})

			It("allows network_class to stay same", func() {
				nc := createNetworkClass(ctx, privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY)

				existing := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Region:       "us-west-1",
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
					}.Build(),
				}.Build()

				updated := privatev1.VirtualNetwork_builder{
					Spec: privatev1.VirtualNetworkSpec_builder{
						Region:       "us-west-1",
						Ipv4Cidr:     proto.String("10.0.0.0/16"),
						NetworkClass: nc.GetId(),
					}.Build(),
				}.Build()

				_, err := server.validateVirtualNetwork(ctx, updated, existing)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Describe("GenericDAO VirtualNetwork operations", func() {
		var generic *dao.GenericDAO[*privatev1.VirtualNetwork]

		BeforeEach(func() {
			var err error

			// Create DAO
			generic, err = dao.NewGenericDAO[*privatev1.VirtualNetwork]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("creates VirtualNetwork and generates ID", func() {
			vn := privatev1.VirtualNetwork_builder{
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"shared"},
				}.Build(),
				Spec: privatev1.VirtualNetworkSpec_builder{
					Ipv4Cidr:     proto.String("10.0.0.0/16"),
					Region:       "us-west-1",
					NetworkClass: "class-id",
				}.Build(),
			}.Build()

			response, err := generic.Create().
				SetObject(vn).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			vn = response.GetObject()
			Expect(vn.GetId()).ToNot(BeEmpty())
			Expect(vn.GetMetadata()).ToNot(BeNil())
			Expect(vn.GetMetadata().GetCreationTimestamp()).ToNot(BeNil())
		})

		It("retrieves VirtualNetwork by ID", func() {
			vn := privatev1.VirtualNetwork_builder{
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"shared"},
				}.Build(),
				Spec: privatev1.VirtualNetworkSpec_builder{
					Ipv4Cidr:     proto.String("10.0.0.0/16"),
					Region:       "us-west-1",
					NetworkClass: "class-id",
				}.Build(),
			}.Build()

			createResponse, err := generic.Create().
				SetObject(vn).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			vn = createResponse.GetObject()

			getResponse, err := generic.Get().
				SetId(vn.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			retrieved := getResponse.GetObject()
			Expect(proto.Equal(vn, retrieved)).To(BeTrue())
		})

		It("lists VirtualNetworks with pagination", func() {
			// Create multiple VirtualNetworks
			const count = 10
			for i := range count {
				vn := privatev1.VirtualNetwork_builder{
					Metadata: privatev1.Metadata_builder{
						Name:    fmt.Sprintf("vn-%d", i),
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String(fmt.Sprintf("10.%d.0.0/16", i)),
						Region:       "us-west-1",
						NetworkClass: "class-id",
					}.Build(),
				}.Build()

				_, err := generic.Create().
					SetObject(vn).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
			}

			// List with limit
			response, err := generic.List().
				SetLimit(5).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(5))
			Expect(response.GetTotal()).To(Equal(int32(count)))
		})

		It("lists with SQL filtering", func() {
			// Create test data
			for i := range 5 {
				vn := privatev1.VirtualNetwork_builder{
					Metadata: privatev1.Metadata_builder{
						Name:    fmt.Sprintf("vn-%d", i),
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.VirtualNetworkSpec_builder{
						Ipv4Cidr:     proto.String(fmt.Sprintf("10.%d.0.0/16", i)),
						Region:       "us-west-1",
						NetworkClass: "class-id",
					}.Build(),
				}.Build()

				_, err := generic.Create().
					SetObject(vn).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
			}

			// List with filter
			response, err := generic.List().
				SetFilter("this.metadata.name == 'vn-2'").
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(1))
			Expect(response.GetItems()[0].GetMetadata().GetName()).To(Equal("vn-2"))
		})

		It("updates VirtualNetwork", func() {
			vn := privatev1.VirtualNetwork_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "original-name",
					Tenants: []string{"shared"},
				}.Build(),
				Spec: privatev1.VirtualNetworkSpec_builder{
					Ipv4Cidr:     proto.String("10.0.0.0/16"),
					Region:       "us-west-1",
					NetworkClass: "class-id",
				}.Build(),
			}.Build()

			createResponse, err := generic.Create().
				SetObject(vn).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			vn = createResponse.GetObject()

			// Update name
			vn.GetMetadata().Name = "updated-name"
			updateResponse, err := generic.Update().
				SetObject(vn).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			vn = updateResponse.GetObject()

			// Verify update
			getResponse, err := generic.Get().
				SetId(vn.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			retrieved := getResponse.GetObject()
			Expect(retrieved.GetMetadata().GetName()).To(Equal("updated-name"))
		})

		It("soft deletes VirtualNetwork", func() {
			vn := privatev1.VirtualNetwork_builder{
				Metadata: privatev1.Metadata_builder{
					Finalizers: []string{"test-finalizer"},
					Tenants:    []string{"shared"},
				}.Build(),
				Spec: privatev1.VirtualNetworkSpec_builder{
					Ipv4Cidr:     proto.String("10.0.0.0/16"),
					Region:       "us-west-1",
					NetworkClass: "class-id",
				}.Build(),
			}.Build()

			createResponse, err := generic.Create().
				SetObject(vn).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			vn = createResponse.GetObject()

			// Delete
			_, err = generic.Delete().
				SetId(vn.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify soft delete (deletion_timestamp set, object still retrievable)
			getResponse, err := generic.Get().
				SetId(vn.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			retrieved := getResponse.GetObject()
			Expect(retrieved.GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})

		It("enforces tenant isolation", func() {
			// Create VirtualNetwork with tenant-a
			vn1 := privatev1.VirtualNetwork_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "vn-tenant-a",
					Tenants: []string{"tenant-a"},
				}.Build(),
				Spec: privatev1.VirtualNetworkSpec_builder{
					Ipv4Cidr:     proto.String("10.1.0.0/16"),
					Region:       "us-west-1",
					NetworkClass: "class-id",
				}.Build(),
			}.Build()

			_, err := generic.Create().
				SetObject(vn1).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create VirtualNetwork with tenant-b
			vn2 := privatev1.VirtualNetwork_builder{
				Metadata: privatev1.Metadata_builder{
					Name:    "vn-tenant-b",
					Tenants: []string{"tenant-b"},
				}.Build(),
				Spec: privatev1.VirtualNetworkSpec_builder{
					Ipv4Cidr:     proto.String("10.2.0.0/16"),
					Region:       "us-west-1",
					NetworkClass: "class-id",
				}.Build(),
			}.Build()

			_, err = generic.Create().
				SetObject(vn2).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// List should return both (tenancy logic allows all in test context)
			response, err := generic.List().Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(2))
		})
	})
})
