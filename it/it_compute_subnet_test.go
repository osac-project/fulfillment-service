/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package it

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("ComputeInstance with Subnet attachment", func() {
	var (
		ctx                            context.Context
		subnetsClient                  privatev1.SubnetsClient
		virtualNetworksClient          privatev1.VirtualNetworksClient
		networkClassesClient           privatev1.NetworkClassesClient
		computeInstancesClient         publicv1.ComputeInstancesClient
		computeInstanceTemplatesClient privatev1.ComputeInstanceTemplatesClient

		networkClassId            string
		virtualNetworkId          string
		subnetId                  string
		computeInstanceId         string
		computeInstanceTemplateId string
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Create clients
		subnetsClient = privatev1.NewSubnetsClient(tool.InternalView().AdminConn())
		virtualNetworksClient = privatev1.NewVirtualNetworksClient(tool.InternalView().AdminConn())
		networkClassesClient = privatev1.NewNetworkClassesClient(tool.InternalView().AdminConn())
		computeInstancesClient = publicv1.NewComputeInstancesClient(tool.ExternalView().UserConn())
		computeInstanceTemplatesClient = privatev1.NewComputeInstanceTemplatesClient(tool.InternalView().AdminConn())

		// Create ComputeInstanceTemplate
		computeInstanceTemplateId = fmt.Sprintf("test-ci-template-%s", uuid.New())
		_, err := computeInstanceTemplatesClient.Create(ctx, privatev1.ComputeInstanceTemplatesCreateRequest_builder{
			Object: privatev1.ComputeInstanceTemplate_builder{
				Metadata:    sharedMetadata(),
				Id:          computeInstanceTemplateId,
				Title:       "Test CI Template",
				Description: "Template for compute instance subnet test.",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Create NetworkClass
		ncResp, err := networkClassesClient.Create(ctx, privatev1.NetworkClassesCreateRequest_builder{
			Object: privatev1.NetworkClass_builder{
				Metadata:               sharedMetadata(),
				Title:                  "Test CUDN Network Class",
				ImplementationStrategy: "cudn",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		networkClassId = ncResp.GetObject().GetId()

		// Create VirtualNetwork
		virtualNetworkId = fmt.Sprintf("test-vnet-%s", uuid.New())
		_, err = virtualNetworksClient.Create(ctx, privatev1.VirtualNetworksCreateRequest_builder{
			Object: privatev1.VirtualNetwork_builder{
				Metadata: sharedMetadata(),
				Id:       virtualNetworkId,
				Spec: privatev1.VirtualNetworkSpec_builder{
					NetworkClass: networkClassId,
					Region:       "us-east-1",
					Ipv4Cidr:     new("10.100.0.0/16"),
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Set VirtualNetwork to READY state via private Update API
		// In IT environment there is no osac-operator/feedback controller to reconcile state
		vnGetResp, err := virtualNetworksClient.Get(ctx, privatev1.VirtualNetworksGetRequest_builder{
			Id: virtualNetworkId,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		vn := vnGetResp.GetObject()
		vn.SetStatus(privatev1.VirtualNetworkStatus_builder{
			State: privatev1.VirtualNetworkState_VIRTUAL_NETWORK_STATE_READY,
		}.Build())
		_, err = virtualNetworksClient.Update(ctx, privatev1.VirtualNetworksUpdateRequest_builder{
			Object:     vn,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status.state"}},
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Create Subnet
		subnetId = fmt.Sprintf("test-subnet-%s", uuid.New())
		_, err = subnetsClient.Create(ctx, privatev1.SubnetsCreateRequest_builder{
			Object: privatev1.Subnet_builder{
				Metadata: sharedMetadata(),
				Id:       subnetId,
				Spec: privatev1.SubnetSpec_builder{
					VirtualNetwork: virtualNetworkId,
					Ipv4Cidr:       new("10.100.1.0/24"),
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Set Subnet to READY state via private Update API
		subGetResp, err := subnetsClient.Get(ctx, privatev1.SubnetsGetRequest_builder{
			Id: subnetId,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		sub := subGetResp.GetObject()
		sub.SetStatus(privatev1.SubnetStatus_builder{
			State: privatev1.SubnetState_SUBNET_STATE_READY,
		}.Build())
		_, err = subnetsClient.Update(ctx, privatev1.SubnetsUpdateRequest_builder{
			Object:     sub,
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status.state"}},
		}.Build())
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// Clean up ComputeInstance if created
		if computeInstanceId != "" {
			computeInstancesClient.Delete(ctx, publicv1.ComputeInstancesDeleteRequest_builder{
				Id: computeInstanceId,
			}.Build())
		}

		// Clean up Subnet
		if subnetId != "" {
			subnetsClient.Delete(ctx, privatev1.SubnetsDeleteRequest_builder{
				Id: subnetId,
			}.Build())
		}

		// Clean up VirtualNetwork
		if virtualNetworkId != "" {
			virtualNetworksClient.Delete(ctx, privatev1.VirtualNetworksDeleteRequest_builder{
				Id: virtualNetworkId,
			}.Build())
		}

		// Clean up NetworkClass
		if networkClassId != "" {
			networkClassesClient.Delete(ctx, privatev1.NetworkClassesDeleteRequest_builder{
				Id: networkClassId,
			}.Build())
		}

		// Clean up ComputeInstanceTemplate
		if computeInstanceTemplateId != "" {
			computeInstanceTemplatesClient.Delete(ctx, privatev1.ComputeInstanceTemplatesDeleteRequest_builder{
				Id: computeInstanceTemplateId,
			}.Build())
		}
	})

	It("creates ComputeInstance with network attachments", func() {
		// Create ComputeInstance with network attachment
		computeInstanceId = fmt.Sprintf("test-ci-%s", uuid.New())
		createResp, err := computeInstancesClient.Create(ctx, publicv1.ComputeInstancesCreateRequest_builder{
			Object: publicv1.ComputeInstance_builder{
				Id: computeInstanceId,
				Spec: publicv1.ComputeInstanceSpec_builder{
					Template:    computeInstanceTemplateId,
					Cores:       new(int32(2)),
					MemoryGib:   new(int32(4)),
					RunStrategy: new("Always"),
					BootDisk: publicv1.ComputeInstanceDisk_builder{
						SizeGib: 20,
					}.Build(),
					Image: publicv1.ComputeInstanceImage_builder{
						SourceType: "registry",
						SourceRef:  "quay.io/containerdisks/fedora:latest",
					}.Build(),
					NetworkAttachments: []*publicv1.NetworkAttachment{
						publicv1.NetworkAttachment_builder{
							Subnet: subnetId,
						}.Build(),
					},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(createResp.GetObject()).ToNot(BeNil())

		// Verify the network attachment is persisted via Get
		getResp, err := computeInstancesClient.Get(ctx, publicv1.ComputeInstancesGetRequest_builder{
			Id: computeInstanceId,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.GetObject().GetSpec().GetNetworkAttachments()).To(HaveLen(1))
		Expect(getResp.GetObject().GetSpec().GetNetworkAttachments()[0].GetSubnet()).To(Equal(subnetId),
			"ComputeInstance should persist network attachment with subnet reference")
	})

	It("rejects ComputeInstance with non-existent subnet in network attachments", func() {
		computeInstanceId = fmt.Sprintf("test-ci-%s", uuid.New())
		_, err := computeInstancesClient.Create(ctx, publicv1.ComputeInstancesCreateRequest_builder{
			Object: publicv1.ComputeInstance_builder{
				Id: computeInstanceId,
				Spec: publicv1.ComputeInstanceSpec_builder{
					Template:    computeInstanceTemplateId,
					Cores:       new(int32(2)),
					MemoryGib:   new(int32(4)),
					RunStrategy: new("Always"),
					BootDisk: publicv1.ComputeInstanceDisk_builder{
						SizeGib: 20,
					}.Build(),
					Image: publicv1.ComputeInstanceImage_builder{
						SourceType: "registry",
						SourceRef:  "quay.io/containerdisks/fedora:latest",
					}.Build(),
					NetworkAttachments: []*publicv1.NetworkAttachment{
						publicv1.NetworkAttachment_builder{
							Subnet: "non-existent-subnet",
						}.Build(),
					},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		computeInstanceId = "" // not created, skip cleanup
	})
})
