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

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("SecurityGroups server", func() {
	var (
		ctx              context.Context
		tx               database.Tx
		virtualNetworkID string
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
		err = dao.CreateTables[*publicv1.SecurityGroup](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.VirtualNetwork](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.NetworkClass](ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create a default NetworkClass for tests:
		ncDao, err := dao.NewGenericDAO[*privatev1.NetworkClass]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		nc := privatev1.NetworkClass_builder{
			Id:                     "default",
			ImplementationStrategy: "ovn-kubernetes",
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"shared"},
			}.Build(),
			Capabilities: privatev1.NetworkClassCapabilities_builder{
				SupportsIpv4:      true,
				SupportsIpv6:      true,
				SupportsDualStack: true,
			}.Build(),
			Status: privatev1.NetworkClassStatus_builder{
				State: privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY,
			}.Build(),
		}.Build()

		createNCResponse, err := ncDao.Create().
			SetObject(nc).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(createNCResponse.GetObject().GetId()).To(Equal("default"))

		// Create a default VirtualNetwork for tests:
		vnDao, err := dao.NewGenericDAO[*privatev1.VirtualNetwork]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		vn := privatev1.VirtualNetwork_builder{
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"shared"},
			}.Build(),
			Spec: privatev1.VirtualNetworkSpec_builder{
				Region:       "us-east-1",
				NetworkClass: "default",
				Ipv4Cidr:     proto.String("10.0.0.0/16"),
				Capabilities: privatev1.VirtualNetworkCapabilities_builder{
					EnableIpv4: true,
				}.Build(),
			}.Build(),
			Status: privatev1.VirtualNetworkStatus_builder{
				State: privatev1.VirtualNetworkState_VIRTUAL_NETWORK_STATE_READY,
			}.Build(),
		}.Build()

		createVNResponse, err := vnDao.Create().
			SetObject(vn).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		virtualNetworkID = createVNResponse.GetObject().GetId()
		Expect(virtualNetworkID).ToNot(BeEmpty())
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewSecurityGroupsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewSecurityGroupsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewSecurityGroupsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var server *SecurityGroupsServer

		BeforeEach(func() {
			var err error

			// Create the server:
			server, err = NewSecurityGroupsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Creates object", func() {
			response, err := server.Create(ctx, publicv1.SecurityGroupsCreateRequest_builder{
				Object: publicv1.SecurityGroup_builder{
					Spec: publicv1.SecurityGroupSpec_builder{
						VirtualNetwork: virtualNetworkID,
						Ingress: []*publicv1.SecurityRule{
							{
								Protocol: publicv1.Protocol_PROTOCOL_TCP,
								PortFrom: proto.Int32(80),
								PortTo:   proto.Int32(80),
								Ipv4Cidr: proto.String("0.0.0.0/0"),
							},
						},
						Egress: []*publicv1.SecurityRule{
							{
								Protocol: publicv1.Protocol_PROTOCOL_ALL,
								Ipv4Cidr: proto.String("0.0.0.0/0"),
							},
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())
			Expect(object.GetSpec().GetIngress()).To(HaveLen(1))
			Expect(object.GetSpec().GetEgress()).To(HaveLen(1))
		})

		It("List objects", func() {
			// Create a few objects:
			const count = 10
			for i := range count {
				_, err := server.Create(ctx, publicv1.SecurityGroupsCreateRequest_builder{
					Object: publicv1.SecurityGroup_builder{
						Metadata: publicv1.Metadata_builder{
							Name: fmt.Sprintf("sg-%d", i),
						}.Build(),
						Spec: publicv1.SecurityGroupSpec_builder{
							VirtualNetwork: virtualNetworkID,
							Ingress: []*publicv1.SecurityRule{
								{
									Protocol: publicv1.Protocol_PROTOCOL_TCP,
									PortFrom: proto.Int32(443),
									PortTo:   proto.Int32(443),
									Ipv4Cidr: proto.String("0.0.0.0/0"),
								},
							},
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, publicv1.SecurityGroupsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			// Create a few objects:
			const count = 10
			for range count {
				_, err := server.Create(ctx, publicv1.SecurityGroupsCreateRequest_builder{
					Object: publicv1.SecurityGroup_builder{
						Spec: publicv1.SecurityGroupSpec_builder{
							VirtualNetwork: virtualNetworkID,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, publicv1.SecurityGroupsListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			// Create a few objects:
			const count = 10
			for range count {
				_, err := server.Create(ctx, publicv1.SecurityGroupsCreateRequest_builder{
					Object: publicv1.SecurityGroup_builder{
						Spec: publicv1.SecurityGroupSpec_builder{
							VirtualNetwork: virtualNetworkID,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, publicv1.SecurityGroupsListRequest_builder{
				Offset: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-1))
		})

		It("List objects with filter", func() {
			// Create a few objects:
			const count = 10
			var objects []*publicv1.SecurityGroup
			for range count {
				response, err := server.Create(ctx, publicv1.SecurityGroupsCreateRequest_builder{
					Object: publicv1.SecurityGroup_builder{
						Spec: publicv1.SecurityGroupSpec_builder{
							VirtualNetwork: virtualNetworkID,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				objects = append(objects, response.GetObject())
			}

			// List the objects:
			for _, object := range objects {
				response, err := server.List(ctx, publicv1.SecurityGroupsListRequest_builder{
					Filter: proto.String(fmt.Sprintf("this.id == '%s'", object.GetId())),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 1))
				Expect(response.GetItems()[0].GetId()).To(Equal(object.GetId()))
			}
		})

		It("Get object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, publicv1.SecurityGroupsCreateRequest_builder{
				Object: publicv1.SecurityGroup_builder{
					Spec: publicv1.SecurityGroupSpec_builder{
						VirtualNetwork: virtualNetworkID,
						Ingress: []*publicv1.SecurityRule{
							{
								Protocol: publicv1.Protocol_PROTOCOL_TCP,
								PortFrom: proto.Int32(22),
								PortTo:   proto.Int32(22),
								Ipv4Cidr: proto.String("10.0.0.0/8"),
							},
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get it:
			getResponse, err := server.Get(ctx, publicv1.SecurityGroupsGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(createResponse.GetObject(), getResponse.GetObject())).To(BeTrue())
		})

		It("Update object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, publicv1.SecurityGroupsCreateRequest_builder{
				Object: publicv1.SecurityGroup_builder{
					Metadata: publicv1.Metadata_builder{
						Name: "original-name",
					}.Build(),
					Spec: publicv1.SecurityGroupSpec_builder{
						VirtualNetwork: virtualNetworkID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Update the object:
			updateResponse, err := server.Update(ctx, publicv1.SecurityGroupsUpdateRequest_builder{
				Object: publicv1.SecurityGroup_builder{
					Id: object.GetId(),
					Metadata: publicv1.Metadata_builder{
						Name: "updated-name",
					}.Build(),
					Spec: publicv1.SecurityGroupSpec_builder{
						VirtualNetwork: virtualNetworkID,
						Ingress: []*publicv1.SecurityRule{
							{
								Protocol: publicv1.Protocol_PROTOCOL_UDP,
								PortFrom: proto.Int32(53),
								PortTo:   proto.Int32(53),
								Ipv4Cidr: proto.String("0.0.0.0/0"),
							},
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetMetadata().GetName()).To(Equal("updated-name"))
			Expect(updateResponse.GetObject().GetSpec().GetIngress()).To(HaveLen(1))

			// Get and verify:
			getResponse, err := server.Get(ctx, publicv1.SecurityGroupsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetName()).To(Equal("updated-name"))
		})

		It("Delete object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, publicv1.SecurityGroupsCreateRequest_builder{
				Object: publicv1.SecurityGroup_builder{
					Spec: publicv1.SecurityGroupSpec_builder{
						VirtualNetwork: virtualNetworkID,
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
				`update security_groups set finalizers = '{"a"}' where id = $1`,
				object.GetId(),
			)
			Expect(err).ToNot(HaveOccurred())

			// Delete the object:
			_, err = server.Delete(ctx, publicv1.SecurityGroupsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get and verify:
			getResponse, err := server.Get(ctx, publicv1.SecurityGroupsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})
	})
})
