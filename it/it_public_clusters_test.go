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
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	ffv1 "github.com/osac-project/fulfillment-service/internal/api/fulfillment/v1"
	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	sharedv1 "github.com/osac-project/fulfillment-service/internal/api/shared/v1"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/gvks"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("Public clusters", func() {
	var (
		ctx               context.Context
		clustersClient    ffv1.ClustersClient
		hostClassesClient privatev1.HostClassesClient
		templatesClient   privatev1.ClusterTemplatesClient
		hostClassId       string
		templateId        string
	)

	BeforeEach(func() {
		// Create a context:
		ctx = context.Background()

		// Create the clients:
		clustersClient = ffv1.NewClustersClient(tool.UserConn())
		hostClassesClient = privatev1.NewHostClassesClient(tool.AdminConn())
		templatesClient = privatev1.NewClusterTemplatesClient(tool.AdminConn())

		// Create a host class for testing:
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

		// Create a template for testing:
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

	It("Can get a specific cluster", func() {
		// Create the cluster
		createResponse, err := clustersClient.Create(ctx, ffv1.ClustersCreateRequest_builder{
			Object: ffv1.Cluster_builder{
				Spec: ffv1.ClusterSpec_builder{
					Template: templateId,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()

		// Delete the cluster after the test:
		DeferCleanup(func() {
			_, err := clustersClient.Delete(ctx, ffv1.ClustersDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		// Get the cluster and verify that the returned data is correct
		response, err := clustersClient.Get(ctx, ffv1.ClustersGetRequest_builder{
			Id: object.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object = response.GetObject()
		metadata := object.GetMetadata()
		Expect(metadata).ToNot(BeNil())
		Expect(metadata.HasCreationTimestamp()).To(BeTrue())
		Expect(metadata.HasDeletionTimestamp()).To(BeFalse())
		spec := object.GetSpec()
		Expect(spec).ToNot(BeNil())
		Expect(spec.GetTemplate()).To(Equal(templateId))
	})

	It("Can get the list of clusters", func() {
		// Create a cluster
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

		// Get the list of clusters and verify that it isn't empty:
		listResponse, err := clustersClient.List(ctx, ffv1.ClustersListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(listResponse).ToNot(BeNil())
		items := listResponse.GetItems()
		Expect(items).ToNot(BeNil())
	})

	It("Can create a cluster", func() {
		// Create the cluster:
		response, err := clustersClient.Create(ctx, ffv1.ClustersCreateRequest_builder{
			Object: ffv1.Cluster_builder{
				Spec: ffv1.ClusterSpec_builder{
					Template: templateId,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		object := response.GetObject()
		DeferCleanup(func() {
			_, err := clustersClient.Delete(ctx, ffv1.ClustersDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		// Verify that the cluster has been created correctly:
		Expect(object).ToNot(BeNil())
		metadata := object.GetMetadata()
		Expect(metadata).ToNot(BeNil())
		Expect(metadata.HasCreationTimestamp()).To(BeTrue())
		Expect(metadata.HasDeletionTimestamp()).To(BeFalse())
		spec := object.GetSpec()
		Expect(spec).ToNot(BeNil())
		Expect(spec.GetTemplate()).To(Equal(templateId))
	})

	It("Can update a cluster", func() {
		// Create the cluster
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

		// Update the cluster:
		updateResponse, err := clustersClient.Update(ctx, ffv1.ClustersUpdateRequest_builder{
			Object: ffv1.Cluster_builder{
				Id: object.GetId(),
				Spec: ffv1.ClusterSpec_builder{
					Template: templateId,
					NodeSets: map[string]*ffv1.ClusterNodeSet{
						"my-node-set": {
							HostClass: hostClassId,
							Size:      4,
						},
					},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object = updateResponse.GetObject()
		nodeSet := object.GetSpec().GetNodeSets()["my-node-set"]
		Expect(nodeSet).ToNot(BeNil())
		Expect(nodeSet.GetSize()).To(BeNumerically("==", 4))

		// Get the cluster and verify that the returned data is correct
		getResponse, err := clustersClient.Get(ctx, ffv1.ClustersGetRequest_builder{
			Id: object.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object = getResponse.GetObject()
		nodeSet = object.GetSpec().GetNodeSets()["my-node-set"]
		Expect(nodeSet).ToNot(BeNil())
		Expect(nodeSet.GetSize()).To(BeNumerically("==", 4))
	})

	It("Can delete a cluster", func() {
		// Create the cluster
		createResponse, err := clustersClient.Create(ctx, ffv1.ClustersCreateRequest_builder{
			Object: ffv1.Cluster_builder{
				Spec: ffv1.ClusterSpec_builder{
					Template: templateId,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()
		_, err = clustersClient.Delete(ctx, ffv1.ClustersDeleteRequest_builder{
			Id: object.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Trying to get the deleted object should either fail if the object has been completely deleted and
		// archived, or return an object that has the deletion timestamp set.
		getResponse, err := clustersClient.Get(ctx, ffv1.ClustersGetRequest_builder{
			Id: object.GetId(),
		}.Build())
		if err != nil {
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.NotFound))
		} else {
			object = getResponse.GetObject()
			metadata := object.GetMetadata()
			Expect(metadata.HasDeletionTimestamp()).To(BeTrue())
		}
	})

	It("Returns not found error when getting cluster that doesn't exist", func() {
		_, err := clustersClient.Get(ctx, ffv1.ClustersGetRequest_builder{
			Id: "does-not-exist",
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})

	It("Returns not found error when updating cluster that doesn't exist", func() {
		_, err := clustersClient.Update(ctx, ffv1.ClustersUpdateRequest_builder{
			Object: ffv1.Cluster_builder{
				Id: "does-not-exist",
				Metadata: sharedv1.Metadata_builder{
					Name: "my-name",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})

	It("Returns not found error when deleting cluster that doesn't exist", func() {
		_, err := clustersClient.Delete(ctx, ffv1.ClustersDeleteRequest_builder{
			Id: "does-not-exist",
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})

	It("Can get the kubeconfig of a cluster", func() {
		// Create the cluster
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

		// Wait till the Kubernetes object has been created:
		kubeClient := tool.KubeClient()
		clusterOrderList := &unstructured.UnstructuredList{}
		clusterOrderList.SetGroupVersionKind(gvks.ClusterOrderList)
		var clusterOrderObj *unstructured.Unstructured
		Eventually(
			func(g Gomega) {
				err := kubeClient.List(ctx, clusterOrderList, crclient.MatchingLabels{
					labels.ClusterOrderUuid: object.GetId(),
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(clusterOrderList.Items).To(HaveLen(1))
				clusterOrderObj = &clusterOrderList.Items[0]
			},
			time.Minute,
			time.Second,
		).Should(Succeed())

		// Create the Hypershift namespace:
		namespaceObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterOrderObj.GetName(),
			},
		}
		err = kubeClient.Create(ctx, namespaceObj)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := kubeClient.Delete(ctx, namespaceObj)
			Expect(err).ToNot(HaveOccurred())
		})

		// Create the Hypershift kubeconfig secret:
		secretObj := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespaceObj.GetName(),
				Name:      "kubeconfig",
			},
			Data: map[string][]byte{
				"kubeconfig": []byte("my-kubeconfig"),
			},
		}
		err = kubeClient.Create(ctx, secretObj)
		Expect(err).ToNot(HaveOccurred())

		// Create the Hypershift hosted cluster, and set the reference to the kubeconfig secret:
		hostedClusterObj := &unstructured.Unstructured{}
		hostedClusterObj.SetGroupVersionKind(gvks.HostedCluster)
		hostedClusterObj.SetNamespace(namespaceObj.GetName())
		hostedClusterObj.SetName(clusterOrderObj.GetName())
		err = kubeClient.Create(ctx, hostedClusterObj)
		Expect(err).ToNot(HaveOccurred())
		hostedClusterUpdate := hostedClusterObj.DeepCopy()
		hostedClusterUpdate.Object["status"] = map[string]any{
			"kubeconfig": map[string]any{
				"name": secretObj.GetName(),
			},
		}
		Expect(err).ToNot(HaveOccurred())
		hostedClusterPatch := crclient.MergeFrom(hostedClusterObj)
		err = kubeClient.Status().Patch(ctx, hostedClusterUpdate, hostedClusterPatch)
		Expect(err).ToNot(HaveOccurred())

		// Save the reference to the hosted cluster in the cluster order:
		clusterOrderUpdate := clusterOrderObj.DeepCopy()
		clusterOrderUpdate.Object["status"] = map[string]any{
			"clusterReference": map[string]any{
				"namespace":         hostedClusterObj.GetNamespace(),
				"hostedClusterName": hostedClusterObj.GetName(),
			},
		}
		Expect(err).ToNot(HaveOccurred())
		clusterOrderPatch := crclient.MergeFrom(clusterOrderObj)
		err = kubeClient.Status().Patch(ctx, clusterOrderUpdate, clusterOrderPatch)
		Expect(err).ToNot(HaveOccurred())

		// Get the kubeconfig:
		var kubeconfig string
		Eventually(
			func(g Gomega) {
				getKubeconfigResponse, err := clustersClient.GetKubeconfig(
					ctx,
					ffv1.ClustersGetKubeconfigRequest_builder{
						Id: object.GetId(),
					}.Build(),
				)
				g.Expect(err).ToNot(HaveOccurred())
				kubeconfig = getKubeconfigResponse.GetKubeconfig()
				g.Expect(kubeconfig).To(Equal("my-kubeconfig"))
			},
			time.Minute,
			time.Second,
		).Should(Succeed())
	})

	It("Can get the kubeconfig of a cluster via HTTP", func() {
		// Create the cluster
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

		// Wait till the Kubernetes object has been created:
		kubeClient := tool.KubeClient()
		clusterOrderList := &unstructured.UnstructuredList{}
		clusterOrderList.SetGroupVersionKind(gvks.ClusterOrderList)
		var clusterOrderObj *unstructured.Unstructured
		Eventually(
			func(g Gomega) {
				err := kubeClient.List(ctx, clusterOrderList, crclient.MatchingLabels{
					labels.ClusterOrderUuid: object.GetId(),
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(clusterOrderList.Items).To(HaveLen(1))
				clusterOrderObj = &clusterOrderList.Items[0]
			},
			time.Minute,
			time.Second,
		).Should(Succeed())

		// Create the Hypershift namespace:
		namespaceObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterOrderObj.GetName(),
			},
		}
		err = kubeClient.Create(ctx, namespaceObj)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := kubeClient.Delete(ctx, namespaceObj)
			Expect(err).ToNot(HaveOccurred())
		})

		// Create the Hypershift kubeconfig secret:
		secretObj := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespaceObj.GetName(),
				Name:      "kubeconfig",
			},
			Data: map[string][]byte{
				"kubeconfig": []byte("my-kubeconfig"),
			},
		}
		err = kubeClient.Create(ctx, secretObj)
		Expect(err).ToNot(HaveOccurred())

		// Create the Hypershift hosted cluster, and set the reference to the kubeconfig secret:
		hostedClusterObj := &unstructured.Unstructured{}
		hostedClusterObj.SetGroupVersionKind(gvks.HostedCluster)
		hostedClusterObj.SetNamespace(namespaceObj.GetName())
		hostedClusterObj.SetName(clusterOrderObj.GetName())
		err = kubeClient.Create(ctx, hostedClusterObj)
		Expect(err).ToNot(HaveOccurred())
		hostedClusterUpdate := hostedClusterObj.DeepCopy()
		hostedClusterUpdate.Object["status"] = map[string]any{
			"kubeconfig": map[string]any{
				"name": secretObj.GetName(),
			},
		}
		Expect(err).ToNot(HaveOccurred())
		hostedClusterPatch := crclient.MergeFrom(hostedClusterObj)
		err = kubeClient.Status().Patch(ctx, hostedClusterUpdate, hostedClusterPatch)
		Expect(err).ToNot(HaveOccurred())

		// Save the reference to the hosted cluster in the cluster order:
		clusterOrderUpdate := clusterOrderObj.DeepCopy()
		clusterOrderUpdate.Object["status"] = map[string]any{
			"clusterReference": map[string]any{
				"namespace":         hostedClusterObj.GetNamespace(),
				"hostedClusterName": hostedClusterObj.GetName(),
			},
		}
		Expect(err).ToNot(HaveOccurred())
		clusterOrderPatch := crclient.MergeFrom(clusterOrderObj)
		err = kubeClient.Status().Patch(ctx, clusterOrderUpdate, clusterOrderPatch)
		Expect(err).ToNot(HaveOccurred())

		// Get the kubeconfig:
		Eventually(
			func(g Gomega) {
				getKubeconfigResponse, err := clustersClient.GetKubeconfigViaHttp(
					ctx,
					ffv1.ClustersGetKubeconfigViaHttpRequest_builder{
						Id: object.GetId(),
					}.Build(),
				)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(getKubeconfigResponse.GetContentType()).To(Equal("application/yaml"))
				g.Expect(getKubeconfigResponse.GetData()).To(Equal([]byte("my-kubeconfig")))
			},
			time.Minute,
			time.Second,
		).Should(Succeed())
	})

	It("Can get the admin password of a cluster", func() {
		// Create the cluster
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

		// Wait till the Kubernetes object has been created:
		kubeClient := tool.KubeClient()
		clusterOrderList := &unstructured.UnstructuredList{}
		clusterOrderList.SetGroupVersionKind(gvks.ClusterOrderList)
		var clusterOrderObj *unstructured.Unstructured
		Eventually(
			func(g Gomega) {
				err := kubeClient.List(ctx, clusterOrderList, crclient.MatchingLabels{
					labels.ClusterOrderUuid: object.GetId(),
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(clusterOrderList.Items).To(HaveLen(1))
				clusterOrderObj = &clusterOrderList.Items[0]
			},
			time.Minute,
			time.Second,
		).Should(Succeed())

		// Create the Hypershift namespace:
		namespaceObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterOrderObj.GetName(),
			},
		}
		err = kubeClient.Create(ctx, namespaceObj)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := kubeClient.Delete(ctx, namespaceObj)
			Expect(err).ToNot(HaveOccurred())
		})

		// Create the Hypershift admin password secret:
		secretObj := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespaceObj.GetName(),
				Name:      "password",
			},
			Data: map[string][]byte{
				"password": []byte("my_password"),
			},
		}
		err = kubeClient.Create(ctx, secretObj)
		Expect(err).ToNot(HaveOccurred())

		// Create the Hypershift hosted cluster, and set the reference to the admin password secret:
		hostedClusterObj := &unstructured.Unstructured{}
		hostedClusterObj.SetGroupVersionKind(gvks.HostedCluster)
		hostedClusterObj.SetNamespace(namespaceObj.GetName())
		hostedClusterObj.SetName(clusterOrderObj.GetName())
		err = kubeClient.Create(ctx, hostedClusterObj)
		Expect(err).ToNot(HaveOccurred())
		hostedClusterUpdate := hostedClusterObj.DeepCopy()
		hostedClusterUpdate.Object["status"] = map[string]any{
			"kubeadminPassword": map[string]any{
				"name": secretObj.GetName(),
			},
		}
		Expect(err).ToNot(HaveOccurred())
		hostedClusterPatch := crclient.MergeFrom(hostedClusterObj)
		err = kubeClient.Status().Patch(ctx, hostedClusterUpdate, hostedClusterPatch)
		Expect(err).ToNot(HaveOccurred())

		// Save the reference to the hosted cluster in the cluster order:
		clusterOrderUpdate := clusterOrderObj.DeepCopy()
		clusterOrderUpdate.Object["status"] = map[string]any{
			"clusterReference": map[string]any{
				"namespace":         hostedClusterObj.GetNamespace(),
				"hostedClusterName": hostedClusterObj.GetName(),
			},
		}
		Expect(err).ToNot(HaveOccurred())
		clusterOrderPatch := crclient.MergeFrom(clusterOrderObj)
		err = kubeClient.Status().Patch(ctx, clusterOrderUpdate, clusterOrderPatch)
		Expect(err).ToNot(HaveOccurred())

		// Get the admin password (with Eventually to handle cache propagation delay)
		var password string
		Eventually(
			func(g Gomega) {
				getPasswordResponse, err := clustersClient.GetPassword(ctx, ffv1.ClustersGetPasswordRequest_builder{
					Id: object.GetId(),
				}.Build())
				g.Expect(err).ToNot(HaveOccurred())
				password = getPasswordResponse.GetPassword()
				g.Expect(password).To(Equal("my_password"))
			},
			30*time.Second,
			time.Second,
		).Should(Succeed())
	})

	It("Can get the admin password of a cluster via HTTP", func() {
		// Create the cluster
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

		// Wait till the Kubernetes object has been created:
		kubeClient := tool.KubeClient()
		clusterOrderList := &unstructured.UnstructuredList{}
		clusterOrderList.SetGroupVersionKind(gvks.ClusterOrderList)
		var clusterOrderObj *unstructured.Unstructured
		Eventually(
			func(g Gomega) {
				err := kubeClient.List(ctx, clusterOrderList, crclient.MatchingLabels{
					labels.ClusterOrderUuid: object.GetId(),
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(clusterOrderList.Items).To(HaveLen(1))
				clusterOrderObj = &clusterOrderList.Items[0]
			},
			time.Minute,
			time.Second,
		).Should(Succeed())

		// Create the Hypershift namespace:
		namespaceObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterOrderObj.GetName(),
			},
		}
		err = kubeClient.Create(ctx, namespaceObj)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := kubeClient.Delete(ctx, namespaceObj)
			Expect(err).ToNot(HaveOccurred())
		})

		// Create the Hypershift admin password secret:
		secretObj := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespaceObj.GetName(),
				Name:      "password",
			},
			Data: map[string][]byte{
				"password": []byte("my_password"),
			},
		}
		err = kubeClient.Create(ctx, secretObj)
		Expect(err).ToNot(HaveOccurred())

		// Create the Hypershift hosted cluster, and set the reference to the admin password secret:
		hostedClusterObj := &unstructured.Unstructured{}
		hostedClusterObj.SetGroupVersionKind(gvks.HostedCluster)
		hostedClusterObj.SetNamespace(namespaceObj.GetName())
		hostedClusterObj.SetName(clusterOrderObj.GetName())
		err = kubeClient.Create(ctx, hostedClusterObj)
		Expect(err).ToNot(HaveOccurred())
		hostedClusterUpdate := hostedClusterObj.DeepCopy()
		hostedClusterUpdate.Object["status"] = map[string]any{
			"kubeadminPassword": map[string]any{
				"name": secretObj.GetName(),
			},
		}
		Expect(err).ToNot(HaveOccurred())
		hostedClusterPatch := crclient.MergeFrom(hostedClusterObj)
		err = kubeClient.Status().Patch(ctx, hostedClusterUpdate, hostedClusterPatch)
		Expect(err).ToNot(HaveOccurred())

		// Save the reference to the hosted cluster in the cluster order:
		clusterOrderUpdate := clusterOrderObj.DeepCopy()
		clusterOrderUpdate.Object["status"] = map[string]any{
			"clusterReference": map[string]any{
				"namespace":         hostedClusterObj.GetNamespace(),
				"hostedClusterName": hostedClusterObj.GetName(),
			},
		}
		Expect(err).ToNot(HaveOccurred())
		clusterOrderPatch := crclient.MergeFrom(clusterOrderObj)
		err = kubeClient.Status().Patch(ctx, clusterOrderUpdate, clusterOrderPatch)
		Expect(err).ToNot(HaveOccurred())

		// Get the admin password (with Eventually to handle cache propagation delay)
		Eventually(
			func(g Gomega) {
				getPasswordResponse, err := clustersClient.GetPasswordViaHttp(
					ctx,
					ffv1.ClustersGetPasswordViaHttpRequest_builder{
						Id: object.GetId(),
					}.Build(),
				)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(getPasswordResponse.GetContentType()).To(Equal("text/plain"))
				g.Expect(getPasswordResponse.GetData()).To(Equal([]byte("my_password")))
			},
			time.Minute,
			time.Second,
		).Should(Succeed())
	})

	It("Sets creator to the name of the user when creating a cluster", func() {
		// Create the cluster using the client connection:
		response, err := clustersClient.Create(ctx, ffv1.ClustersCreateRequest_builder{
			Object: ffv1.Cluster_builder{
				Spec: ffv1.ClusterSpec_builder{
					Template: templateId,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		object := response.GetObject()
		DeferCleanup(func() {
			_, err := clustersClient.Delete(ctx, ffv1.ClustersDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		// Verify that the creator is set to the name of the authenticated user:
		Expect(object).ToNot(BeNil())
		metadata := object.GetMetadata()
		Expect(metadata).ToNot(BeNil())
		Expect(metadata.GetCreators()).To(ConsistOf(userUsername))
	})
})
