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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	ffv1 "github.com/innabox/fulfillment-service/internal/api/fulfillment/v1"
	privatev1 "github.com/innabox/fulfillment-service/internal/api/private/v1"
	sharedv1 "github.com/innabox/fulfillment-service/internal/api/shared/v1"
	"github.com/innabox/fulfillment-service/internal/uuid"
)

var _ = Describe("Annotations", func() {
	var (
		ctx               context.Context
		clustersClient    ffv1.ClustersClient
		hostClassesClient privatev1.HostClassesClient
		templatesClient   privatev1.ClusterTemplatesClient
		hostClassId       string
		templateId        string
	)

	BeforeEach(func() {
		ctx = context.Background()
		clustersClient = ffv1.NewClustersClient(tool.UserConn())
		hostClassesClient = privatev1.NewHostClassesClient(tool.AdminConn())
		templatesClient = privatev1.NewClusterTemplatesClient(tool.AdminConn())

		hostClassId = fmt.Sprintf("my-host-class-%s", uuid.New())
		_, err := hostClassesClient.Create(ctx, privatev1.HostClassesCreateRequest_builder{
			Object: privatev1.HostClass_builder{
				Id: hostClassId,
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, err := hostClassesClient.Delete(ctx, privatev1.HostClassesDeleteRequest_builder{
				Id: hostClassId,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		templateId = fmt.Sprintf("my-template-%s", uuid.New())
		_, err = templatesClient.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
			Object: privatev1.ClusterTemplate_builder{
				Id:          templateId,
				Title:       "My template %s",
				Description: "My template.",
				NodeSets: map[string]*privatev1.ClusterTemplateNodeSet{
					"my-node-set": privatev1.ClusterTemplateNodeSet_builder{
						HostClass: hostClassId,
						Size:      3,
					}.Build(),
				},
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, err := templatesClient.Delete(ctx, privatev1.ClusterTemplatesDeleteRequest_builder{
				Id: templateId,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})
	})

	It("Can create a cluster with annotations", func() {
		createResponse, err := clustersClient.Create(ctx, ffv1.ClustersCreateRequest_builder{
			Object: ffv1.Cluster_builder{
				Metadata: sharedv1.Metadata_builder{
					Annotations: map[string]string{
						"example.com/annotation": "my-annotation",
						"simple":                 "value",
					},
				}.Build(),
				Spec: ffv1.ClusterSpec_builder{
					Template: templateId,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()
		DeferCleanup(func() {
			_, err := clustersClient.Delete(ctx, ffv1.ClustersDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})
		annotations := object.GetMetadata().GetAnnotations()
		Expect(annotations).To(HaveKeyWithValue("example.com/annotation", "my-annotation"))
		Expect(annotations).To(HaveKeyWithValue("simple", "value"))

		getResponse, err := clustersClient.Get(ctx, ffv1.ClustersGetRequest_builder{
			Id: object.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object = getResponse.GetObject()
		annotations = object.GetMetadata().GetAnnotations()
		Expect(annotations).To(HaveKeyWithValue("example.com/annotation", "my-annotation"))
		Expect(annotations).To(HaveKeyWithValue("simple", "value"))
	})

	It("Can update a cluster with annotations", func() {
		createResponse, err := clustersClient.Create(ctx, ffv1.ClustersCreateRequest_builder{
			Object: ffv1.Cluster_builder{
				Spec: ffv1.ClusterSpec_builder{
					Template: templateId,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()
		DeferCleanup(func() {
			_, err := clustersClient.Delete(ctx, ffv1.ClustersDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		updateResponse, err := clustersClient.Update(ctx, ffv1.ClustersUpdateRequest_builder{
			Object: ffv1.Cluster_builder{
				Id: object.GetId(),
				Metadata: sharedv1.Metadata_builder{
					Annotations: map[string]string{
						"example.com/updated": "updated-annotation",
						"another":             "second",
					},
				}.Build(),
				Spec: ffv1.ClusterSpec_builder{
					Template: templateId,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object = updateResponse.GetObject()
		annotations := object.GetMetadata().GetAnnotations()
		Expect(annotations).To(HaveKeyWithValue("example.com/updated", "updated-annotation"))
		Expect(annotations).To(HaveKeyWithValue("another", "second"))

		getResponse, err := clustersClient.Get(ctx, ffv1.ClustersGetRequest_builder{
			Id: object.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object = getResponse.GetObject()
		annotations = object.GetMetadata().GetAnnotations()
		Expect(annotations).To(HaveKeyWithValue("example.com/updated", "updated-annotation"))
		Expect(annotations).To(HaveKeyWithValue("another", "second"))
	})

	DescribeTable(
		"Rejects invalid annotations on create and update",
		func(key string, expected string) {
			By("Creating a cluster with an invalid annotation key", func() {
				_, err := clustersClient.Create(ctx, ffv1.ClustersCreateRequest_builder{
					Object: ffv1.Cluster_builder{
						Metadata: sharedv1.Metadata_builder{
							Annotations: map[string]string{
								key: "",
							},
						}.Build(),
						Spec: ffv1.ClusterSpec_builder{
							Template: templateId,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(status.Message()).To(Equal(expected))
			})

			By("Updating a cluster with a valid annotation key", func() {
				createResponse, err := clustersClient.Create(ctx, ffv1.ClustersCreateRequest_builder{
					Object: ffv1.Cluster_builder{
						Spec: ffv1.ClusterSpec_builder{
							Template: templateId,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				object := createResponse.GetObject()
				DeferCleanup(func() {
					_, err := clustersClient.Delete(ctx, ffv1.ClustersDeleteRequest_builder{
						Id: object.GetId(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())
				})
				_, err = clustersClient.Update(ctx, ffv1.ClustersUpdateRequest_builder{
					Object: ffv1.Cluster_builder{
						Id: object.GetId(),
						Metadata: sharedv1.Metadata_builder{
							Annotations: map[string]string{
								key: "",
							},
						}.Build(),
						Spec: ffv1.ClusterSpec_builder{
							Template: templateId,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(status.Message()).To(Equal(expected))
			})
		},
		Entry(
			"Invalid annotation name character",
			"bad^annotation",
			"field 'metadata.annotations' key 'bad^annotation' name must only contain lowercase letters "+
				"(a-z), digits (0-9), hyphens (-), underscores (_) or dots (.), but contains '^' at position 3",
		),
		Entry(
			"Invalid annotation prefix character",
			"bad_prefix/annotation",
			"field 'metadata.annotations' key 'bad_prefix/annotation' prefix segment must only contain "+
				"lowercase letters (a-z), digits (0-9) and hyphens (-), but contains '_' at position 3",
		),
	)
})
