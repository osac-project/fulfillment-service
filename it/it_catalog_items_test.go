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
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/structpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("Catalog items", func() {
	var (
		ctx                    context.Context
		privateClusterClient   privatev1.ClusterCatalogItemsClient
		privateComputeClient   privatev1.ComputeInstanceCatalogItemsClient
		publicClusterClient    publicv1.ClusterCatalogItemsClient
		clusterTemplatesClient privatev1.ClusterTemplatesClient
		computeTemplatesClient privatev1.ComputeInstanceTemplatesClient
	)

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		privateClusterClient = privatev1.NewClusterCatalogItemsClient(tool.InternalView().AdminConn())
		privateComputeClient = privatev1.NewComputeInstanceCatalogItemsClient(tool.InternalView().AdminConn())
		publicClusterClient = publicv1.NewClusterCatalogItemsClient(tool.ExternalView().UserConn())
		clusterTemplatesClient = privatev1.NewClusterTemplatesClient(tool.InternalView().AdminConn())
		computeTemplatesClient = privatev1.NewComputeInstanceTemplatesClient(tool.InternalView().AdminConn())
	})

	Describe("Cluster catalog items", func() {
		var templateID string

		BeforeEach(func() {
			templateID = fmt.Sprintf("test_template_%s", uuid.New())
			_, err := clusterTemplatesClient.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
				Object: privatev1.ClusterTemplate_builder{
					Id:          templateID,
					Title:       "Test Template",
					Description: "A test template for catalog items",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Can create a published cluster catalog item via private API", func() {
			name := fmt.Sprintf("test-catalog-item-%s", uuid.New())

			response, err := privateClusterClient.Create(ctx, privatev1.ClusterCatalogItemsCreateRequest_builder{
				Object: privatev1.ClusterCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Name: name,
					}.Build(),
					Title:       "Simple OpenShift Cluster",
					Description: "A test cluster catalog item",
					Template:    templateID,
					Published:   true,
					Tenant:      "",
					FieldDefinitions: []*privatev1.FieldDefinition{
						privatev1.FieldDefinition_builder{
							Path:        "spec.network.pod_cidr",
							DisplayName: "Pod CIDR",
							Editable:    true,
						}.Build(),
					},
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())

			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetMetadata().GetName()).To(Equal(name))
			Expect(object.GetTitle()).To(Equal("Simple OpenShift Cluster"))
			Expect(object.GetTemplate()).To(Equal(templateID))
			Expect(object.GetPublished()).To(BeTrue())
			Expect(object.GetTenant()).To(Equal(""))
			Expect(object.GetFieldDefinitions()).To(HaveLen(1))
		})

		It("Published catalog items appear in public API", func() {
			name := fmt.Sprintf("test-catalog-item-%s", uuid.New())

			_, err := privateClusterClient.Create(ctx, privatev1.ClusterCatalogItemsCreateRequest_builder{
				Object: privatev1.ClusterCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Name: name,
					}.Build(),
					Title:       "Public Cluster Catalog Item",
					Description: "Should be visible in public API",
					Template:    templateID,
					Published:   true,
					Tenant:      "",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			listResponse, err := publicClusterClient.List(ctx, publicv1.ClusterCatalogItemsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(listResponse).ToNot(BeNil())

			items := listResponse.GetItems()
			found := false
			for _, item := range items {
				if item.GetMetadata().GetName() == name {
					found = true
					Expect(item.GetTitle()).To(Equal("Public Cluster Catalog Item"))
					Expect(item.GetPublished()).To(BeTrue())
					break
				}
			}
			Expect(found).To(BeTrue(), "Published catalog item should appear in public API")
		})

		It("Unpublished catalog items do not appear in public API", func() {
			name := fmt.Sprintf("test-catalog-item-%s", uuid.New())

			_, err := privateClusterClient.Create(ctx, privatev1.ClusterCatalogItemsCreateRequest_builder{
				Object: privatev1.ClusterCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Name: name,
					}.Build(),
					Title:       "Unpublished Cluster Catalog Item",
					Description: "Should NOT be visible in public API",
					Template:    templateID,
					Published:   false,
					Tenant:      "",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			listResponse, err := publicClusterClient.List(ctx, publicv1.ClusterCatalogItemsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(listResponse).ToNot(BeNil())

			items := listResponse.GetItems()
			for _, item := range items {
				Expect(item.GetMetadata().GetName()).ToNot(Equal(name), "Unpublished item should not appear in public API")
			}
		})

		It("Can create catalog items with field definitions and defaults", func() {
			name := fmt.Sprintf("test-catalog-item-%s", uuid.New())

			defaultCIDR, err := structpb.NewValue("10.128.0.0/14")
			Expect(err).ToNot(HaveOccurred())

			response, err := privateClusterClient.Create(ctx, privatev1.ClusterCatalogItemsCreateRequest_builder{
				Object: privatev1.ClusterCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Name: name,
					}.Build(),
					Title:       "Cluster with Field Definitions",
					Description: "Catalog item with editable fields and validation",
					Template:    templateID,
					Published:   true,
					Tenant:      "",
					FieldDefinitions: []*privatev1.FieldDefinition{
						privatev1.FieldDefinition_builder{
							Path:             "spec.network.pod_cidr",
							DisplayName:      "Pod CIDR",
							Editable:         true,
							Default:          defaultCIDR,
							ValidationSchema: `{"type": "string", "pattern": "^([0-9]{1,3}\\.){3}[0-9]{1,3}/[0-9]{1,2}$"}`,
						}.Build(),
					},
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			object := response.GetObject()
			fieldDefs := object.GetFieldDefinitions()
			Expect(fieldDefs).To(HaveLen(1))
			Expect(fieldDefs[0].GetPath()).To(Equal("spec.network.pod_cidr"))
			Expect(fieldDefs[0].GetDisplayName()).To(Equal("Pod CIDR"))
			Expect(fieldDefs[0].GetEditable()).To(BeTrue())
			Expect(fieldDefs[0].GetDefault().GetStringValue()).To(Equal("10.128.0.0/14"))
			Expect(fieldDefs[0].GetValidationSchema()).To(ContainSubstring("pattern"))
		})
	})

	Describe("Compute instance catalog items", func() {
		var templateID string

		BeforeEach(func() {
			templateID = fmt.Sprintf("test_vm_template_%s", uuid.New())
			_, err := computeTemplatesClient.Create(ctx, privatev1.ComputeInstanceTemplatesCreateRequest_builder{
				Object: privatev1.ComputeInstanceTemplate_builder{
					Id:          templateID,
					Title:       "Test VM Template",
					Description: "A test VM template for catalog items",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Can create a published compute instance catalog item via private API", func() {
			name := fmt.Sprintf("test-vm-catalog-item-%s", uuid.New())

			defaultCores, err := structpb.NewValue(float64(8))
			Expect(err).ToNot(HaveOccurred())
			defaultMemory, err := structpb.NewValue(float64(64))
			Expect(err).ToNot(HaveOccurred())

			response, err := privateComputeClient.Create(ctx, privatev1.ComputeInstanceCatalogItemsCreateRequest_builder{
				Object: privatev1.ComputeInstanceCatalogItem_builder{
					Metadata: privatev1.Metadata_builder{
						Name: name,
					}.Build(),
					Title:       "Linux Virtual Machine",
					Description: "A test VM catalog item",
					Template:    templateID,
					Published:   true,
					Tenant:      "",
					FieldDefinitions: []*privatev1.FieldDefinition{
						privatev1.FieldDefinition_builder{
							Path:             "spec.cores",
							DisplayName:      "CPU Cores",
							Editable:         true,
							Default:          defaultCores,
							ValidationSchema: `{"type": "integer", "minimum": 1, "maximum": 64}`,
						}.Build(),
						privatev1.FieldDefinition_builder{
							Path:             "spec.memory_gib",
							DisplayName:      "Memory (GiB)",
							Editable:         true,
							Default:          defaultMemory,
							ValidationSchema: `{"type": "integer", "minimum": 1, "maximum": 512}`,
						}.Build(),
					},
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())

			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetMetadata().GetName()).To(Equal(name))
			Expect(object.GetTitle()).To(Equal("Linux Virtual Machine"))
			Expect(object.GetTemplate()).To(Equal(templateID))
			Expect(object.GetPublished()).To(BeTrue())
			Expect(object.GetFieldDefinitions()).To(HaveLen(2))
		})

	})
})
