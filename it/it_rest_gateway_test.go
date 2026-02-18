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
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("REST gateway", func() {
	var (
		ctx               context.Context
		templatesClient   privatev1.ClusterTemplatesClient
		hostClassesClient privatev1.HostClassesClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		templatesClient = privatev1.NewClusterTemplatesClient(tool.AdminConn())
		hostClassesClient = privatev1.NewHostClassesClient(tool.AdminConn())
	})

	It("Should use protobuf field names in JSON representation", func() {
		// Create a couple of host classes for the node sets:
		computeHostClassID := fmt.Sprintf("compute_%s", uuid.New())
		gpuHostClassID := fmt.Sprintf("gpus_%s", uuid.New())
		_, err := hostClassesClient.Create(ctx, privatev1.HostClassesCreateRequest_builder{
			Object: privatev1.HostClass_builder{
				Id:          computeHostClassID,
				Title:       "Compute",
				Description: "Compute.",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, err := hostClassesClient.Delete(ctx, privatev1.HostClassesDeleteRequest_builder{
				Id: computeHostClassID,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})
		_, err = hostClassesClient.Create(ctx, privatev1.HostClassesCreateRequest_builder{
			Object: privatev1.HostClass_builder{
				Id:          gpuHostClassID,
				Title:       "GPU",
				Description: "GPU.",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, err := hostClassesClient.Delete(ctx, privatev1.HostClassesDeleteRequest_builder{
				Id: gpuHostClassID,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		// Create a cluster template:
		templateID := fmt.Sprintf("my_%s", uuid.New())
		nodeSets := map[string]*privatev1.ClusterTemplateNodeSet{
			"compute": privatev1.ClusterTemplateNodeSet_builder{
				HostClass: computeHostClassID,
				Size:      3,
			}.Build(),
			"gpu": privatev1.ClusterTemplateNodeSet_builder{
				HostClass: gpuHostClassID,
				Size:      2,
			}.Build(),
		}
		_, err = templatesClient.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
			Object: privatev1.ClusterTemplate_builder{
				Id:          templateID,
				Title:       "My template",
				Description: "My template.",
				NodeSets:    nodeSets,
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, err := templatesClient.Delete(ctx, privatev1.ClusterTemplatesDeleteRequest_builder{
				Id: templateID,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		// Retrieve the template via REST API:
		url := fmt.Sprintf("https://%s/api/fulfillment/v1/cluster_templates/%s", serviceAddr, templateID)
		request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		Expect(err).ToNot(HaveOccurred())
		response, err := tool.UserClient().Do(request)
		Expect(err).ToNot(HaveOccurred())
		defer response.Body.Close()
		Expect(response.StatusCode).To(Equal(http.StatusOK))
		data, err := io.ReadAll(response.Body)
		Expect(err).ToNot(HaveOccurred())
		var body map[string]any
		err = json.Unmarshal(data, &body)
		Expect(err).ToNot(HaveOccurred())

		// Verify field names:
		Expect(body).To(HaveKey("node_sets"))
	})

	It("Should be possible to fetch cluster templates via private REST API", func() {
		// Create a couple of host classes for the node sets:
		computeHostClassID := fmt.Sprintf("compute_%s", uuid.New())
		gpuHostClassID := fmt.Sprintf("gpus_%s", uuid.New())
		_, err := hostClassesClient.Create(ctx, privatev1.HostClassesCreateRequest_builder{
			Object: privatev1.HostClass_builder{
				Id:          computeHostClassID,
				Title:       "Compute",
				Description: "Compute.",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, err := hostClassesClient.Delete(ctx, privatev1.HostClassesDeleteRequest_builder{
				Id: computeHostClassID,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})
		_, err = hostClassesClient.Create(ctx, privatev1.HostClassesCreateRequest_builder{
			Object: privatev1.HostClass_builder{
				Id:          gpuHostClassID,
				Title:       "GPU",
				Description: "GPU.",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, err := hostClassesClient.Delete(ctx, privatev1.HostClassesDeleteRequest_builder{
				Id: gpuHostClassID,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		// Create a cluster template:
		templateID := fmt.Sprintf("my_%s", uuid.New())
		nodeSets := map[string]*privatev1.ClusterTemplateNodeSet{
			"compute": privatev1.ClusterTemplateNodeSet_builder{
				HostClass: computeHostClassID,
				Size:      3,
			}.Build(),
			"gpu": privatev1.ClusterTemplateNodeSet_builder{
				HostClass: gpuHostClassID,
				Size:      2,
			}.Build(),
		}
		_, err = templatesClient.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
			Object: privatev1.ClusterTemplate_builder{
				Id:          templateID,
				Title:       "My private template",
				Description: "My private template.",
				NodeSets:    nodeSets,
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, err := templatesClient.Delete(ctx, privatev1.ClusterTemplatesDeleteRequest_builder{
				Id: templateID,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		// Retrieve the template via private REST API:
		url := fmt.Sprintf("https://%s/api/private/v1/cluster_templates/%s", serviceAddr, templateID)
		request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		Expect(err).ToNot(HaveOccurred())
		response, err := tool.AdminClient().Do(request)
		Expect(err).ToNot(HaveOccurred())
		defer response.Body.Close()
		Expect(response.StatusCode).To(Equal(http.StatusOK))
		data, err := io.ReadAll(response.Body)
		Expect(err).ToNot(HaveOccurred())
		var body map[string]any
		err = json.Unmarshal(data, &body)
		Expect(err).ToNot(HaveOccurred())

		// Verify the response contains the expected fields:
		Expect(body).To(HaveKey("id"))
		Expect(body).To(HaveKey("title"))
		Expect(body).To(HaveKey("description"))
		Expect(body).To(HaveKey("node_sets"))
		Expect(body["id"]).To(Equal(templateID))
		Expect(body["title"]).To(Equal("My private template"))
		Expect(body["description"]).To(Equal("My private template."))

		// Verify node_sets structure:
		nodeSetsMap, ok := body["node_sets"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(nodeSetsMap).To(HaveKey("compute"))
		Expect(nodeSetsMap).To(HaveKey("gpu"))
	})
})
