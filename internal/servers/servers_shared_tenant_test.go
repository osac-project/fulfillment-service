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
	"go.uber.org/mock/gomock"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Shared tenant creation acceptance", func() {
	type sharedTenantTestCase struct {
		name   string
		create func() error
		setup  func()
	}

	entries := func() []TableEntry {
		return []TableEntry{
			Entry("Hubs", sharedTenantTestCase{
				name: "Hubs",
				create: func() error {
					server, err := NewPrivateHubsServer().
						SetLogger(logger).
						SetAttributionLogic(attribution).
						SetTenancyLogic(tenancy).
						Build()
					if err != nil {
						return err
					}
					_, err = server.Create(ctx, privatev1.HubsCreateRequest_builder{
						Object: privatev1.Hub_builder{
							Metadata: privatev1.Metadata_builder{
								Tenant: auth.SharedTenant,
							}.Build(),
						}.Build(),
					}.Build())
					return err
				},
			}),
			Entry("HostTypes", sharedTenantTestCase{
				name: "HostTypes",
				create: func() error {
					server, err := NewPrivateHostTypesServer().
						SetLogger(logger).
						SetAttributionLogic(attribution).
						SetTenancyLogic(tenancy).
						Build()
					if err != nil {
						return err
					}
					_, err = server.Create(ctx, privatev1.HostTypesCreateRequest_builder{
						Object: privatev1.HostType_builder{
							Metadata: privatev1.Metadata_builder{
								Tenant: auth.SharedTenant,
							}.Build(),
							Title: "Shared host type",
						}.Build(),
					}.Build())
					return err
				},
			}),
			Entry("Roles", sharedTenantTestCase{
				name: "Roles",
				create: func() error {
					server, err := NewPrivateRolesServer().
						SetLogger(logger).
						SetAttributionLogic(attribution).
						SetTenancyLogic(tenancy).
						Build()
					if err != nil {
						return err
					}
					_, err = server.Create(ctx, privatev1.RolesCreateRequest_builder{
						Object: privatev1.Role_builder{
							Metadata: privatev1.Metadata_builder{
								Tenant: auth.SharedTenant,
							}.Build(),
						}.Build(),
					}.Build())
					return err
				},
			}),
			Entry("ClusterCatalogItems", sharedTenantTestCase{
				name: "ClusterCatalogItems",
				create: func() error {
					server, err := NewPrivateClusterCatalogItemsServer().
						SetLogger(logger).
						SetAttributionLogic(attribution).
						SetTenancyLogic(tenancy).
						Build()
					if err != nil {
						return err
					}
					_, err = server.Create(ctx, privatev1.ClusterCatalogItemsCreateRequest_builder{
						Object: privatev1.ClusterCatalogItem_builder{
							Metadata: privatev1.Metadata_builder{
								Tenant: auth.SharedTenant,
							}.Build(),
							Title:    "Shared catalog item",
							Template: "some-template",
						}.Build(),
					}.Build())
					return err
				},
			}),
			Entry("ClusterTemplates", sharedTenantTestCase{
				name: "ClusterTemplates",
				create: func() error {
					server, err := NewPrivateClusterTemplatesServer().
						SetLogger(logger).
						SetAttributionLogic(attribution).
						SetTenancyLogic(tenancy).
						Build()
					if err != nil {
						return err
					}
					_, err = server.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
						Object: privatev1.ClusterTemplate_builder{
							Metadata: privatev1.Metadata_builder{
								Tenant: auth.SharedTenant,
							}.Build(),
							Title:       "Shared template",
							Description: "A shared cluster template",
						}.Build(),
					}.Build())
					return err
				},
			}),
			Entry("ComputeInstanceCatalogItems", sharedTenantTestCase{
				name: "ComputeInstanceCatalogItems",
				create: func() error {
					server, err := NewPrivateComputeInstanceCatalogItemsServer().
						SetLogger(logger).
						SetAttributionLogic(attribution).
						SetTenancyLogic(tenancy).
						Build()
					if err != nil {
						return err
					}
					_, err = server.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
						Object: privatev1.ComputeInstanceCatalogItem_builder{
							Metadata: privatev1.Metadata_builder{
								Tenant: auth.SharedTenant,
							}.Build(),
							Title:    "Shared CI catalog item",
							Template: "some-ci-template",
						}.Build(),
					}.Build())
					return err
				},
			}),
			Entry("ComputeInstanceTemplates", sharedTenantTestCase{
				name: "ComputeInstanceTemplates",
				create: func() error {
					server, err := NewPrivateComputeInstanceTemplatesServer().
						SetLogger(logger).
						SetAttributionLogic(attribution).
						SetTenancyLogic(tenancy).
						Build()
					if err != nil {
						return err
					}
					_, err = server.Create(ctx, privatev1.ComputeInstanceTemplatesCreateRequest_builder{
						Object: privatev1.ComputeInstanceTemplate_builder{
							Metadata: privatev1.Metadata_builder{
								Tenant: auth.SharedTenant,
							}.Build(),
							Title:       "Shared CI template",
							Description: "A shared compute instance template",
						}.Build(),
					}.Build())
					return err
				},
			}),
			Entry("NetworkClasses", sharedTenantTestCase{
				name: "NetworkClasses",
				create: func() error {
					server, err := NewPrivateNetworkClassesServer().
						SetLogger(logger).
						SetAttributionLogic(attribution).
						SetTenancyLogic(tenancy).
						Build()
					if err != nil {
						return err
					}
					_, err = server.Create(ctx, privatev1.NetworkClassesCreateRequest_builder{
						Object: privatev1.NetworkClass_builder{
							Metadata: privatev1.Metadata_builder{
								Tenant: auth.SharedTenant,
							}.Build(),
							Title:                  "Shared network class",
							ImplementationStrategy: "ovn-kubernetes",
						}.Build(),
					}.Build())
					return err
				},
			}),
			Entry("PublicIPPools", sharedTenantTestCase{
				name: "PublicIPPools",
				create: func() error {
					server, err := NewPrivatePublicIPPoolsServer().
						SetLogger(logger).
						SetAttributionLogic(attribution).
						SetTenancyLogic(tenancy).
						Build()
					if err != nil {
						return err
					}
					_, err = server.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
						Object: privatev1.PublicIPPool_builder{
							Metadata: privatev1.Metadata_builder{
								Tenant: auth.SharedTenant,
							}.Build(),
							Spec: privatev1.PublicIPPoolSpec_builder{
								Cidrs:    []string{"192.168.0.0/24"},
								IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
							}.Build(),
						}.Build(),
					}.Build())
					return err
				},
			}),
		}
	}

	DescribeTable("accepts creation in the shared tenant",
		func(tc sharedTenantTestCase) {
			if tc.setup != nil {
				tc.setup()
			}
			Expect(tc.create()).To(Succeed())
		},
		entries(),
	)
})

var _ = Describe("Reserved tenant creation restriction", func() {
	Describe("Cluster", func() {
		var templateID string

		BeforeEach(func() {
			templateID = "shared-tenant-test-template"

			// Create a cluster template via DAO (bypasses server restriction):
			templatesDao, err := dao.NewGenericDAO[*privatev1.ClusterTemplate]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			_, err = templatesDao.Create().
				SetObject(privatev1.ClusterTemplate_builder{
					Id:          templateID,
					Title:       "Shared tenant test template",
					Description: "Template for shared tenant restriction tests",
					Metadata: privatev1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Rejects creation with explicit shared tenant", func() {
			clustersServer, err := NewClustersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetScheme(testScheme).
				Build()
			Expect(err).ToNot(HaveOccurred())

			response, err := clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
				Object: publicv1.Cluster_builder{
					Metadata: publicv1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Spec: publicv1.ClusterSpec_builder{
						Template: templateID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(response).To(BeNil())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(ContainSubstring("shared"))
			Expect(status.Message()).To(ContainSubstring("not allowed"))
		})

		It("Rejects creation when default tenant resolves to shared", func() {
			// Create a tenancy that simulates an admin whose default is "shared":
			localCtrl := gomock.NewController(GinkgoT())
			DeferCleanup(localCtrl.Finish)

			sharedTenancy := auth.NewMockTenancyLogic(localCtrl)
			sharedTenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
				Return(auth.AllTenants, nil).
				AnyTimes()
			sharedTenancy.EXPECT().DetermineDefaultTenant(gomock.Any()).
				Return(auth.SharedTenant, nil).
				AnyTimes()
			sharedTenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
				Return(auth.AllTenants, nil).
				AnyTimes()

			clustersServer, err := NewClustersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(sharedTenancy).
				SetScheme(testScheme).
				Build()
			Expect(err).ToNot(HaveOccurred())

			response, err := clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
				Object: publicv1.Cluster_builder{
					Spec: publicv1.ClusterSpec_builder{
						Template: templateID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(response).To(BeNil())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(ContainSubstring("shared"))
			Expect(status.Message()).To(ContainSubstring("not allowed"))
		})

		It("Allows creation in a non-shared tenant", func() {
			clustersServer, err := NewClustersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetScheme(testScheme).
				Build()
			Expect(err).ToNot(HaveOccurred())

			response, err := clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
				Object: publicv1.Cluster_builder{
					Spec: publicv1.ClusterSpec_builder{
						Template: templateID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.GetObject().GetMetadata().GetTenant()).To(Equal("test-tenant"))
		})

		It("Rejects creation with explicit system tenant", func() {
			clustersServer, err := NewClustersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetScheme(testScheme).
				Build()
			Expect(err).ToNot(HaveOccurred())

			response, err := clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
				Object: publicv1.Cluster_builder{
					Metadata: publicv1.Metadata_builder{
						Tenant: auth.SystemTenant,
					}.Build(),
					Spec: publicv1.ClusterSpec_builder{
						Template: templateID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(response).To(BeNil())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(ContainSubstring("system"))
			Expect(status.Message()).To(ContainSubstring("not allowed"))
		})

		It("Rejects update that moves object to shared tenant", func() {
			clustersServer, err := NewClustersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetScheme(testScheme).
				Build()
			Expect(err).ToNot(HaveOccurred())

			createResponse, err := clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
				Object: publicv1.Cluster_builder{
					Spec: publicv1.ClusterSpec_builder{
						Template: templateID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			clusterID := createResponse.GetObject().GetId()

			updateResponse, err := clustersServer.Update(ctx, publicv1.ClustersUpdateRequest_builder{
				Object: publicv1.Cluster_builder{
					Id: clusterID,
					Metadata: publicv1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(updateResponse).To(BeNil())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(ContainSubstring("shared"))
			Expect(status.Message()).To(ContainSubstring("not allowed"))
		})
	})

	Describe("VirtualNetwork", func() {
		BeforeEach(func() {
			// Create a default NetworkClass via DAO:
			ncDao, err := dao.NewGenericDAO[*privatev1.NetworkClass]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			_, err = ncDao.Create().
				SetObject(privatev1.NetworkClass_builder{
					Id:                     "default",
					ImplementationStrategy: "ovn-kubernetes",
					Metadata: privatev1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					IsDefault: proto.Bool(true),
					Capabilities: privatev1.NetworkClassCapabilities_builder{
						SupportsIpv4:      true,
						SupportsIpv6:      true,
						SupportsDualStack: true,
					}.Build(),
					Status: privatev1.NetworkClassStatus_builder{
						State: privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY,
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Rejects creation with explicit shared tenant", func() {
			vnServer, err := NewVirtualNetworksServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			response, err := vnServer.Create(ctx, publicv1.VirtualNetworksCreateRequest_builder{
				Object: publicv1.VirtualNetwork_builder{
					Metadata: publicv1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Spec: publicv1.VirtualNetworkSpec_builder{
						Ipv4Cidr: proto.String("10.0.0.0/16"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(response).To(BeNil())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(ContainSubstring("shared"))
			Expect(status.Message()).To(ContainSubstring("not allowed"))
		})
	})

	Describe("Subnet", func() {
		var virtualNetworkID string

		BeforeEach(func() {
			// Create a NetworkClass via DAO:
			ncDao, err := dao.NewGenericDAO[*privatev1.NetworkClass]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			_, err = ncDao.Create().
				SetObject(privatev1.NetworkClass_builder{
					Id:                     "default",
					ImplementationStrategy: "ovn-kubernetes",
					Metadata: privatev1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					IsDefault: proto.Bool(true),
					Capabilities: privatev1.NetworkClassCapabilities_builder{
						SupportsIpv4:      true,
						SupportsIpv6:      true,
						SupportsDualStack: true,
					}.Build(),
					Status: privatev1.NetworkClassStatus_builder{
						State: privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY,
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create a parent VirtualNetwork via DAO (READY state):
			vnDao, err := dao.NewGenericDAO[*privatev1.VirtualNetwork]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			vnResp, err := vnDao.Create().
				SetObject(privatev1.VirtualNetwork_builder{
					Metadata: privatev1.Metadata_builder{
						Tenant: auth.SharedTenant,
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
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			virtualNetworkID = vnResp.GetObject().GetId()
		})

		It("Rejects creation with explicit shared tenant", func() {
			subnetServer, err := NewSubnetsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			response, err := subnetServer.Create(ctx, publicv1.SubnetsCreateRequest_builder{
				Object: publicv1.Subnet_builder{
					Metadata: publicv1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Spec: publicv1.SubnetSpec_builder{
						VirtualNetwork: virtualNetworkID,
						Ipv4Cidr:       proto.String("10.0.1.0/24"),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(response).To(BeNil())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(ContainSubstring("shared"))
			Expect(status.Message()).To(ContainSubstring("not allowed"))
		})
	})

	Describe("ComputeInstance", func() {
		var templateID string

		BeforeEach(func() {
			templateID = "ci-shared-test-template"

			// Create a subnet so compute instance creation passes network validation:
			subnetsDao, err := dao.NewGenericDAO[*privatev1.Subnet]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			_, err = subnetsDao.Create().SetObject(privatev1.Subnet_builder{
				Id: "test-subnet",
				Metadata: privatev1.Metadata_builder{
					Tenant: auth.SharedTenant,
				}.Build(),
				Spec: privatev1.SubnetSpec_builder{
					VirtualNetwork: "test-vnet",
					Ipv4Cidr:       proto.String("10.0.0.0/24"),
				}.Build(),
				Status: privatev1.SubnetStatus_builder{
					State: privatev1.SubnetState_SUBNET_STATE_READY,
				}.Build(),
			}.Build()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create a ComputeInstanceTemplate via DAO:
			templatesDao, err := dao.NewGenericDAO[*privatev1.ComputeInstanceTemplate]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			cpuDefault, err := anypb.New(wrapperspb.Int32(1))
			Expect(err).ToNot(HaveOccurred())
			memoryDefault, err := anypb.New(wrapperspb.Int32(2))
			Expect(err).ToNot(HaveOccurred())

			_, err = templatesDao.Create().
				SetObject(privatev1.ComputeInstanceTemplate_builder{
					Id:          templateID,
					Title:       "CI shared test template",
					Description: "Template for shared tenant restriction tests",
					Metadata: privatev1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Parameters: []*privatev1.ComputeInstanceTemplateParameterDefinition{
						{
							Name:    "cpu_count",
							Title:   "CPU Count",
							Type:    "type.googleapis.com/google.protobuf.Int32Value",
							Default: cpuDefault,
						},
						{
							Name:    "memory_gb",
							Title:   "Memory (GB)",
							Type:    "type.googleapis.com/google.protobuf.Int32Value",
							Default: memoryDefault,
						},
					},
					SpecDefaults: privatev1.ComputeInstanceTemplateSpecDefaults_builder{
						Cores:     proto.Int32(2),
						MemoryGib: proto.Int32(2),
						Image: privatev1.ComputeInstanceImage_builder{
							SourceType: "registry",
							SourceRef:  "quay.io/containerdisks/fedora:latest",
						}.Build(),
						BootDisk: privatev1.ComputeInstanceDisk_builder{
							SizeGib: 10,
						}.Build(),
						RunStrategy: proto.String("Always"),
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Rejects creation with explicit shared tenant", func() {
			ciServer, err := NewComputeInstancesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			response, err := ciServer.Create(ctx, publicv1.ComputeInstancesCreateRequest_builder{
				Object: publicv1.ComputeInstance_builder{
					Metadata: publicv1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Spec: publicv1.ComputeInstanceSpec_builder{
						Template: templateID,
						NetworkAttachments: []*publicv1.NetworkAttachment{
							publicv1.NetworkAttachment_builder{
								Subnet: "test-subnet",
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(response).To(BeNil())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(ContainSubstring("shared"))
			Expect(status.Message()).To(ContainSubstring("not allowed"))
		})
	})

	Describe("SecurityGroup", func() {
		var virtualNetworkID string

		BeforeEach(func() {
			// Create a NetworkClass via DAO:
			ncDao, err := dao.NewGenericDAO[*privatev1.NetworkClass]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			_, err = ncDao.Create().
				SetObject(privatev1.NetworkClass_builder{
					Id:                     "default",
					ImplementationStrategy: "ovn-kubernetes",
					Metadata: privatev1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					IsDefault: proto.Bool(true),
					Capabilities: privatev1.NetworkClassCapabilities_builder{
						SupportsIpv4:      true,
						SupportsIpv6:      true,
						SupportsDualStack: true,
					}.Build(),
					Status: privatev1.NetworkClassStatus_builder{
						State: privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY,
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create a parent VirtualNetwork via DAO (READY state):
			vnDao, err := dao.NewGenericDAO[*privatev1.VirtualNetwork]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			vnResp, err := vnDao.Create().
				SetObject(privatev1.VirtualNetwork_builder{
					Metadata: privatev1.Metadata_builder{
						Tenant: auth.SharedTenant,
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
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			virtualNetworkID = vnResp.GetObject().GetId()
		})

		It("Rejects creation with explicit shared tenant", func() {
			sgServer, err := NewSecurityGroupsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			response, err := sgServer.Create(ctx, publicv1.SecurityGroupsCreateRequest_builder{
				Object: publicv1.SecurityGroup_builder{
					Metadata: publicv1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Spec: publicv1.SecurityGroupSpec_builder{
						VirtualNetwork: virtualNetworkID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(response).To(BeNil())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(ContainSubstring("shared"))
			Expect(status.Message()).To(ContainSubstring("not allowed"))
		})
	})

	Describe("PublicIPAttachment", func() {
		var publicIPID, computeInstanceID string

		BeforeEach(func() {
			// Create a PublicIPPool via DAO:
			poolDao, err := dao.NewGenericDAO[*privatev1.PublicIPPool]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			poolResp, err := poolDao.Create().
				SetObject(privatev1.PublicIPPool_builder{
					Metadata: privatev1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs: []string{"10.1.0.0/24"},
					}.Build(),
					Status: privatev1.PublicIPPoolStatus_builder{
						State:     privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY,
						Total:     254,
						Allocated: 0,
						Available: 254,
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create a PublicIP in ALLOCATED state via DAO:
			pipDao, err := dao.NewGenericDAO[*privatev1.PublicIP]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			pipResp, err := pipDao.Create().
				SetObject(privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolResp.GetObject().GetId(),
					}.Build(),
					Status: privatev1.PublicIPStatus_builder{
						State: privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED,
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			publicIPID = pipResp.GetObject().GetId()

			// Create a ComputeInstance in RUNNING state via DAO:
			ciDao, err := dao.NewGenericDAO[*privatev1.ComputeInstance]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			ciResp, err := ciDao.Create().
				SetObject(privatev1.ComputeInstance_builder{
					Metadata: privatev1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			computeInstanceID = ciResp.GetObject().GetId()
		})

		It("Rejects creation with explicit shared tenant", func() {
			piaServer, err := NewPublicIPAttachmentsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			response, err := piaServer.Create(ctx, publicv1.PublicIPAttachmentsCreateRequest_builder{
				Object: publicv1.PublicIPAttachment_builder{
					Metadata: publicv1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Spec: publicv1.PublicIPAttachmentSpec_builder{
						PublicIp:        publicIPID,
						ComputeInstance: proto.String(computeInstanceID),
					}.Build(),
				}.Build(),
			}.Build())
			Expect(response).To(BeNil())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(ContainSubstring("shared"))
			Expect(status.Message()).To(ContainSubstring("not allowed"))
		})
	})

	Describe("PublicIP", func() {
		var poolID string

		BeforeEach(func() {
			// Create a PublicIPPool via DAO:
			poolDao, err := dao.NewGenericDAO[*privatev1.PublicIPPool]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			poolResp, err := poolDao.Create().
				SetObject(privatev1.PublicIPPool_builder{
					Metadata: privatev1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs: []string{"10.0.0.0/24"},
					}.Build(),
					Status: privatev1.PublicIPPoolStatus_builder{
						State:     privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY,
						Total:     254,
						Allocated: 0,
						Available: 254,
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			poolID = poolResp.GetObject().GetId()
		})

		It("Rejects creation with explicit shared tenant", func() {
			pipServer, err := NewPublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			response, err := pipServer.Create(ctx, publicv1.PublicIPsCreateRequest_builder{
				Object: publicv1.PublicIP_builder{
					Metadata: publicv1.Metadata_builder{
						Tenant: auth.SharedTenant,
					}.Build(),
					Spec: publicv1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(response).To(BeNil())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(ContainSubstring("shared"))
			Expect(status.Message()).To(ContainSubstring("not allowed"))
		})
	})
})
