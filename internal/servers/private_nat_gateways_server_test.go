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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Private NAT gateways server", func() {
	var vnDao *dao.GenericDAO[*privatev1.VirtualNetwork]

	BeforeEach(func() {
		var err error
		vnDao, err = dao.NewGenericDAO[*privatev1.VirtualNetwork]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	createVirtualNetwork := func() string {
		resp, err := vnDao.Create().SetObject(
			privatev1.VirtualNetwork_builder{
				Metadata: privatev1.Metadata_builder{
					Tenant: auth.SharedTenant,
				}.Build(),
			}.Build(),
		).Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		return resp.GetObject().GetId()
	}

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivateNATGatewaysServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivateNATGatewaysServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivateNATGatewaysServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPrivateNATGatewaysServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("CRUD operations", func() {
		var (
			natGatewaysServer *PrivateNATGatewaysServer
			vnID              string
		)

		BeforeEach(func() {
			var err error
			vnID = createVirtualNetwork()
			natGatewaysServer, err = NewPrivateNATGatewaysServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("creates NATGateway with PENDING initial state", func() {
			response, err := natGatewaysServer.Create(ctx, privatev1.NATGatewaysCreateRequest_builder{
				Object: privatev1.NATGateway_builder{
					Metadata: privatev1.Metadata_builder{Tenant: auth.SharedTenant}.Build(),
					Spec: privatev1.NATGatewaySpec_builder{
						VirtualNetwork: vnID,
						ExternalIp:     "test-external-ip",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetObject().GetStatus().GetState()).To(
				Equal(privatev1.NATGatewayState_NAT_GATEWAY_STATE_PENDING))
		})

		It("overrides client-provided state to PENDING on Create", func() {
			response, err := natGatewaysServer.Create(ctx, privatev1.NATGatewaysCreateRequest_builder{
				Object: privatev1.NATGateway_builder{
					Metadata: privatev1.Metadata_builder{Tenant: auth.SharedTenant}.Build(),
					Spec: privatev1.NATGatewaySpec_builder{
						VirtualNetwork: vnID,
						ExternalIp:     "test-external-ip",
					}.Build(),
					Status: privatev1.NATGatewayStatus_builder{
						State: privatev1.NATGatewayState_NAT_GATEWAY_STATE_READY,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetObject().GetStatus().GetState()).To(
				Equal(privatev1.NATGatewayState_NAT_GATEWAY_STATE_PENDING))
		})

		It("retrieves NATGateway by ID", func() {
			createResponse, err := natGatewaysServer.Create(ctx, privatev1.NATGatewaysCreateRequest_builder{
				Object: privatev1.NATGateway_builder{
					Metadata: privatev1.Metadata_builder{Tenant: auth.SharedTenant}.Build(),
					Spec: privatev1.NATGatewaySpec_builder{
						VirtualNetwork: vnID,
						ExternalIp:     "test-external-ip",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := natGatewaysServer.Get(ctx, privatev1.NATGatewaysGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(createResponse.GetObject(), getResponse.GetObject())).To(BeTrue())
		})

		It("lists NATGateways", func() {
			const count = 3
			for range count {
				vn := createVirtualNetwork()
				_, err := natGatewaysServer.Create(ctx, privatev1.NATGatewaysCreateRequest_builder{
					Object: privatev1.NATGateway_builder{
						Metadata: privatev1.Metadata_builder{Tenant: auth.SharedTenant}.Build(),
						Spec: privatev1.NATGatewaySpec_builder{
							VirtualNetwork: vn,
							ExternalIp:     "test-external-ip",
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			response, err := natGatewaysServer.List(ctx, privatev1.NATGatewaysListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(count))
		})

		It("soft deletes NATGateway", func() {
			createResponse, err := natGatewaysServer.Create(ctx, privatev1.NATGatewaysCreateRequest_builder{
				Object: privatev1.NATGateway_builder{
					Metadata: privatev1.Metadata_builder{
						Finalizers: []string{"test-finalizer"},
						Tenant:     auth.SharedTenant,
					}.Build(),
					Spec: privatev1.NATGatewaySpec_builder{
						VirtualNetwork: vnID,
						ExternalIp:     "test-external-ip",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = natGatewaysServer.Delete(ctx, privatev1.NATGatewaysDeleteRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := natGatewaysServer.Get(ctx, privatev1.NATGatewaysGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})

		It("signals NATGateway", func() {
			createResponse, err := natGatewaysServer.Create(ctx, privatev1.NATGatewaysCreateRequest_builder{
				Object: privatev1.NATGateway_builder{
					Metadata: privatev1.Metadata_builder{Tenant: auth.SharedTenant}.Build(),
					Spec: privatev1.NATGatewaySpec_builder{
						VirtualNetwork: vnID,
						ExternalIp:     "test-external-ip",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = natGatewaysServer.Signal(ctx, privatev1.NATGatewaysSignalRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
