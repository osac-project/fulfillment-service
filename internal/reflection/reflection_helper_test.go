/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package reflection

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/packages"
	"github.com/osac-project/fulfillment-service/internal/testing"
)

var _ = Describe("Reflection helper", func() {
	var (
		ctx        context.Context
		server     *testing.Server
		connection *grpc.ClientConn
	)

	BeforeEach(func() {
		var err error

		// Create a context:
		ctx = context.Background()

		// Create the server:
		server = testing.NewServer()
		DeferCleanup(server.Stop)

		// Create the client connection:
		connection, err = grpc.NewClient(
			server.Address(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(connection.Close)
	})

	Describe("Creation", func() {
		It("Can be created with all the mandatory parameters", func() {
			helper, err := NewHelper().
				SetLogger(logger).
				SetConnection(connection).
				AddPackage(packages.PublicV1, 1).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(helper).ToNot(BeNil())
		})

		It("Can be created with multiple packages", func() {
			helper, err := NewHelper().
				SetLogger(logger).
				SetConnection(connection).
				AddPackage(packages.PublicV1, 1).
				AddPackage(packages.PrivateV1, 0).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(helper).ToNot(BeNil())
		})

		It("Can be created with multiple specified in one call", func() {
			helper, err := NewHelper().
				SetLogger(logger).
				SetConnection(connection).
				AddPackages(map[string]int{
					packages.PrivateV1: 0,
					packages.PublicV1:  1,
				}).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(helper).ToNot(BeNil())
		})

		It("Can't be created without a logger", func() {
			helper, err := NewHelper().
				SetConnection(connection).
				AddPackage(packages.PublicV1, 1).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(helper).To(BeNil())
		})

		It("Can't be created without a connection", func() {
			helper, err := NewHelper().
				SetLogger(logger).
				AddPackage(packages.PublicV1, 1).
				Build()
			Expect(err).To(MatchError("gRPC connection is mandatory"))
			Expect(helper).To(BeNil())
		})

		It("Can't be created without at least one package", func() {
			helper, err := NewHelper().
				SetLogger(logger).
				SetConnection(connection).
				Build()
			Expect(err).To(MatchError("at least one package is mandatory"))
			Expect(helper).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var helper *Helper

		BeforeEach(func() {
			var err error
			helper, err = NewHelper().
				SetLogger(logger).
				SetConnection(connection).
				AddPackage(packages.PublicV1, 1).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Returns object types in singular", func() {
			Expect(helper.Singulars()).To(ConsistOf(
				"cluster",
				"clustertemplate",
				"computeinstance",
				"computeinstancetemplate",
				"host",
				"hostclass",
				"hostpool",
			))
		})

		It("Returns object types in plural", func() {
			Expect(helper.Plurals()).To(ConsistOf(
				"clusters",
				"clustertemplates",
				"computeinstances",
				"computeinstancetemplates",
				"hostclasses",
				"hostpools",
				"hosts",
			))
		})

		DescribeTable(
			"Lookup by object type",
			func(objectType string, expectedFullName string) {
				objectHelper := helper.Lookup(objectType)
				Expect(objectHelper).ToNot(BeNil())
				Expect(string(objectHelper.FullName())).To(Equal(expectedFullName))
			},
			Entry(
				"Cluster in singular",
				"cluster",
				"osac.public.v1.Cluster",
			),
			Entry(
				"Cluster in plural",
				"clusters",
				"osac.public.v1.Cluster",
			),
			Entry(
				"Cluster in singular upper case",
				"CLUSTER",
				"osac.public.v1.Cluster",
			),
			Entry(
				"Host class in plural",
				"hostclasses",
				"osac.public.v1.HostClass",
			),
			Entry(
				"Host in singular",
				"host",
				"osac.public.v1.Host",
			),
			Entry(
				"Host in plural",
				"hosts",
				"osac.public.v1.Host",
			),
			Entry(
				"Host pool in singular",
				"hostpool",
				"osac.public.v1.HostPool",
			),
			Entry(
				"Host pool in plural",
				"hostpools",
				"osac.public.v1.HostPool",
			),
		)

		DescribeTable(
			"Returns descriptor",
			func(objectType string, expectedFullName string) {
				objectHelper := helper.Lookup(objectType)
				Expect(objectHelper).ToNot(BeNil())
				objectDescriptor := objectHelper.Descriptor()
				Expect(objectDescriptor).ToNot(BeNil())
				Expect(string(objectDescriptor.FullName())).To(Equal(expectedFullName))
			},
			Entry(
				"Cluster",
				"cluster",
				"osac.public.v1.Cluster",
			),
			Entry(
				"Cluster template",
				"clustertemplate",
				"osac.public.v1.ClusterTemplate",
			),
			Entry(
				"Host class",
				"hostclass",
				"osac.public.v1.HostClass",
			),
			Entry(
				"Compute instance template",
				"computeinstancetemplate",
				"osac.public.v1.ComputeInstanceTemplate",
			),
			Entry(
				"Compute instance",
				"computeinstance",
				"osac.public.v1.ComputeInstance",
			),
			Entry(
				"Host",
				"host",
				"osac.public.v1.Host",
			),
			Entry(
				"Host pool",
				"hostpool",
				"osac.public.v1.HostPool",
			),
		)

		DescribeTable(
			"Creates instance",
			func(objectType string, expectedInstance proto.Message) {
				objectHelper := helper.Lookup(objectType)
				Expect(objectHelper).ToNot(BeNil())
				actualInstance := objectHelper.Instance()
				Expect(proto.Equal(actualInstance, expectedInstance)).To(BeTrue())
			},
			Entry(
				"Cluster",
				"cluster",
				&publicv1.Cluster{},
			),
			Entry(
				"Cluster template",
				"clustertemplate",
				&publicv1.ClusterTemplate{},
			),
			Entry(
				"Host class",
				"hostclass",
				&publicv1.HostClass{},
			),
			Entry(
				"Host",
				"host",
				&publicv1.Host{},
			),
			Entry(
				"Host pool",
				"hostpool",
				&publicv1.HostPool{},
			),
		)

		It("Invokes get method", func() {
			// Register a clusters server that responds to the get request:
			publicv1.RegisterClustersServer(server.Registrar(), &testing.ClustersServerFuncs{
				GetFunc: func(ctx context.Context, request *publicv1.ClustersGetRequest,
				) (response *publicv1.ClustersGetResponse, err error) {
					defer GinkgoRecover()
					Expect(request.GetId()).To(Equal("123"))
					response = publicv1.ClustersGetResponse_builder{
						Object: publicv1.Cluster_builder{
							Id: "123",
							Status: publicv1.ClusterStatus_builder{
								State: publicv1.ClusterState_CLUSTER_STATE_READY,
							}.Build(),
						}.Build(),
					}.Build()
					return
				},
			})

			// Start the server:
			server.Start()

			// Use the helper to send the request, and verify the response:
			objectHelper := helper.Lookup("cluster")
			Expect(objectHelper).ToNot(BeNil())
			object, err := objectHelper.Get(ctx, "123")
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(object, publicv1.Cluster_builder{
				Id: "123",
				Status: publicv1.ClusterStatus_builder{
					State: publicv1.ClusterState_CLUSTER_STATE_READY,
				}.Build(),
			}.Build())).To(BeTrue())
		})

		It("Invokes list method", func() {
			// Register a clusters server that responds to the list request:
			publicv1.RegisterClustersServer(server.Registrar(), &testing.ClustersServerFuncs{
				ListFunc: func(ctx context.Context, request *publicv1.ClustersListRequest,
				) (response *publicv1.ClustersListResponse, err error) {
					response = publicv1.ClustersListResponse_builder{
						Size:  proto.Int32(2),
						Total: proto.Int32(2),
						Items: []*publicv1.Cluster{
							publicv1.Cluster_builder{
								Id: "123",
							}.Build(),
							publicv1.Cluster_builder{
								Id: "456",
							}.Build(),
						},
					}.Build()
					return
				},
			})

			// Start the server:
			server.Start()

			// Use the helper to send the request, and verify the response:
			objectHelper := helper.Lookup("cluster")
			Expect(objectHelper).ToNot(BeNil())
			listResult, err := objectHelper.List(ctx, ListOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(listResult.Items).To(HaveLen(2))
			Expect(listResult.Total).To(Equal(int32(2)))
			Expect(proto.Equal(
				listResult.Items[0],
				publicv1.Cluster_builder{
					Id: "123",
				}.Build(),
			)).To(BeTrue())
			Expect(proto.Equal(
				listResult.Items[1],
				publicv1.Cluster_builder{
					Id: "456",
				}.Build(),
			)).To(BeTrue())
		})

		It("Invokes create method", func() {
			// Register a clusters server that responds to the create request:
			publicv1.RegisterClustersServer(server.Registrar(), &testing.ClustersServerFuncs{
				CreateFunc: func(ctx context.Context, request *publicv1.ClustersCreateRequest,
				) (response *publicv1.ClustersCreateResponse, err error) {
					defer GinkgoRecover()
					Expect(proto.Equal(
						request.Object,
						publicv1.Cluster_builder{
							Spec: publicv1.ClusterSpec_builder{
								NodeSets: map[string]*publicv1.ClusterNodeSet{
									"xyz": publicv1.ClusterNodeSet_builder{
										HostClass: "acme_1tib",
										Size:      3,
									}.Build(),
								},
							}.Build(),
						}.Build(),
					)).To(BeTrue())
					response = publicv1.ClustersCreateResponse_builder{
						Object: publicv1.Cluster_builder{
							Id: "123",
							Spec: publicv1.ClusterSpec_builder{
								NodeSets: map[string]*publicv1.ClusterNodeSet{
									"xyz": publicv1.ClusterNodeSet_builder{
										HostClass: "acme_1tib",
										Size:      3,
									}.Build(),
								},
							}.Build(),
						}.Build(),
					}.Build()
					return
				},
			})

			// Start the server:
			server.Start()

			// Use the helper to send the request, and verify the response:
			objectHelper := helper.Lookup("cluster")
			Expect(objectHelper).ToNot(BeNil())
			object, err := objectHelper.Create(ctx, publicv1.Cluster_builder{
				Spec: publicv1.ClusterSpec_builder{
					NodeSets: map[string]*publicv1.ClusterNodeSet{
						"xyz": publicv1.ClusterNodeSet_builder{
							HostClass: "acme_1tib",
							Size:      3,
						}.Build(),
					},
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(
				object,
				publicv1.Cluster_builder{
					Id: "123",
					Spec: publicv1.ClusterSpec_builder{
						NodeSets: map[string]*publicv1.ClusterNodeSet{
							"xyz": publicv1.ClusterNodeSet_builder{
								HostClass: "acme_1tib",
								Size:      3,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			)).To(BeTrue())
		})

		It("Invokes update method", func() {
			// Register a clusters server that responds to the update request:
			publicv1.RegisterClustersServer(server.Registrar(), &testing.ClustersServerFuncs{
				UpdateFunc: func(ctx context.Context, request *publicv1.ClustersUpdateRequest,
				) (response *publicv1.ClustersUpdateResponse, err error) {
					defer GinkgoRecover()
					Expect(proto.Equal(
						request.Object,
						publicv1.Cluster_builder{
							Id: "123",
							Spec: publicv1.ClusterSpec_builder{
								NodeSets: map[string]*publicv1.ClusterNodeSet{
									"xyz": publicv1.ClusterNodeSet_builder{
										Size: 3,
									}.Build(),
								},
							}.Build(),
						}.Build(),
					)).To(BeTrue())
					response = publicv1.ClustersUpdateResponse_builder{
						Object: publicv1.Cluster_builder{
							Id: "123",
							Spec: publicv1.ClusterSpec_builder{
								NodeSets: map[string]*publicv1.ClusterNodeSet{
									"xyz": publicv1.ClusterNodeSet_builder{
										HostClass: "acme_1tib",
										Size:      3,
									}.Build(),
								},
							}.Build(),
						}.Build(),
					}.Build()
					return
				},
			})

			// Start the server:
			server.Start()

			// Use the helper to send the request, and verify the response:
			objectHelper := helper.Lookup("cluster")
			Expect(objectHelper).ToNot(BeNil())
			object, err := objectHelper.Update(ctx, publicv1.Cluster_builder{
				Id: "123",
				Spec: publicv1.ClusterSpec_builder{
					NodeSets: map[string]*publicv1.ClusterNodeSet{
						"xyz": publicv1.ClusterNodeSet_builder{
							Size: 3,
						}.Build(),
					},
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(
				object,
				publicv1.Cluster_builder{
					Id: "123",
					Spec: publicv1.ClusterSpec_builder{
						NodeSets: map[string]*publicv1.ClusterNodeSet{
							"xyz": publicv1.ClusterNodeSet_builder{
								HostClass: "acme_1tib",
								Size:      3,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			)).To(BeTrue())
		})

		It("Invokes delete method", func() {
			// Register a clusters server that responds to the delete request:
			publicv1.RegisterClustersServer(server.Registrar(), &testing.ClustersServerFuncs{
				DeleteFunc: func(ctx context.Context, request *publicv1.ClustersDeleteRequest,
				) (response *publicv1.ClustersDeleteResponse, err error) {
					defer GinkgoRecover()
					response = publicv1.ClustersDeleteResponse_builder{}.Build()
					return
				},
			})

			// Start the server:
			server.Start()

			// Use the helper to send the request, and verify the response:
			objectHelper := helper.Lookup("cluster")
			Expect(objectHelper).ToNot(BeNil())
			err := objectHelper.Delete(ctx, "123")
			Expect(err).ToNot(HaveOccurred())
		})

		It("Invokes hosts get method", func() {
			// Register a hosts server that responds to the get request:
			publicv1.RegisterHostsServer(server.Registrar(), &testing.HostsServerFuncs{
				GetFunc: func(ctx context.Context, request *publicv1.HostsGetRequest,
				) (response *publicv1.HostsGetResponse, err error) {
					defer GinkgoRecover()
					Expect(request.GetId()).To(Equal("host-123"))
					response = publicv1.HostsGetResponse_builder{
						Object: publicv1.Host_builder{
							Id: "host-123",
							Status: publicv1.HostStatus_builder{
								PowerState: publicv1.HostPowerState_HOST_POWER_STATE_ON,
							}.Build(),
						}.Build(),
					}.Build()
					return
				},
			})

			// Start the server:
			server.Start()

			// Use the helper to send the request, and verify the response:
			objectHelper := helper.Lookup("host")
			Expect(objectHelper).ToNot(BeNil())
			object, err := objectHelper.Get(ctx, "host-123")
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(object, publicv1.Host_builder{
				Id: "host-123",
				Status: publicv1.HostStatus_builder{
					PowerState: publicv1.HostPowerState_HOST_POWER_STATE_ON,
				}.Build(),
			}.Build())).To(BeTrue())
		})

		It("Invokes hosts delete method", func() {
			// Register a hosts server that responds to the delete request:
			publicv1.RegisterHostsServer(server.Registrar(), &testing.HostsServerFuncs{
				DeleteFunc: func(ctx context.Context, request *publicv1.HostsDeleteRequest,
				) (response *publicv1.HostsDeleteResponse, err error) {
					defer GinkgoRecover()
					Expect(request.GetId()).To(Equal("host-123"))
					response = publicv1.HostsDeleteResponse_builder{}.Build()
					return
				},
			})

			// Start the server:
			server.Start()

			// Use the helper to send the request, and verify the response:
			objectHelper := helper.Lookup("host")
			Expect(objectHelper).ToNot(BeNil())
			err := objectHelper.Delete(ctx, "host-123")
			Expect(err).ToNot(HaveOccurred())
		})

		It("Invokes host pools get method", func() {
			// Register a host pools server that responds to the get request:
			publicv1.RegisterHostPoolsServer(server.Registrar(), &testing.HostPoolsServerFuncs{
				GetFunc: func(ctx context.Context, request *publicv1.HostPoolsGetRequest,
				) (response *publicv1.HostPoolsGetResponse, err error) {
					defer GinkgoRecover()
					Expect(request.GetId()).To(Equal("pool-123"))
					response = publicv1.HostPoolsGetResponse_builder{
						Object: publicv1.HostPool_builder{
							Id: "pool-123",
							Status: publicv1.HostPoolStatus_builder{
								State: publicv1.HostPoolState_HOST_POOL_STATE_READY,
							}.Build(),
						}.Build(),
					}.Build()
					return
				},
			})

			// Start the server:
			server.Start()

			// Use the helper to send the request, and verify the response:
			objectHelper := helper.Lookup("hostpool")
			Expect(objectHelper).ToNot(BeNil())
			object, err := objectHelper.Get(ctx, "pool-123")
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(object, publicv1.HostPool_builder{
				Id: "pool-123",
				Status: publicv1.HostPoolStatus_builder{
					State: publicv1.HostPoolState_HOST_POOL_STATE_READY,
				}.Build(),
			}.Build())).To(BeTrue())
		})

		It("Invokes host pools delete method", func() {
			// Register a host pools server that responds to the delete request:
			publicv1.RegisterHostPoolsServer(server.Registrar(), &testing.HostPoolsServerFuncs{
				DeleteFunc: func(ctx context.Context, request *publicv1.HostPoolsDeleteRequest,
				) (response *publicv1.HostPoolsDeleteResponse, err error) {
					defer GinkgoRecover()
					Expect(request.GetId()).To(Equal("pool-123"))
					response = publicv1.HostPoolsDeleteResponse_builder{}.Build()
					return
				},
			})

			// Start the server:
			server.Start()

			// Use the helper to send the request, and verify the response:
			objectHelper := helper.Lookup("hostpool")
			Expect(objectHelper).ToNot(BeNil())
			err := objectHelper.Delete(ctx, "pool-123")
			Expect(err).ToNot(HaveOccurred())
		})

		It("Returns metadata from get method", func() {
			// Register a clusters server that responds to the get request with metadata:
			publicv1.RegisterClustersServer(server.Registrar(), &testing.ClustersServerFuncs{
				GetFunc: func(ctx context.Context, request *publicv1.ClustersGetRequest,
				) (response *publicv1.ClustersGetResponse, err error) {
					defer GinkgoRecover()
					Expect(request.GetId()).To(Equal("123"))
					response = publicv1.ClustersGetResponse_builder{
						Object: publicv1.Cluster_builder{
							Id: "123",
							Metadata: publicv1.Metadata_builder{
								Name: "my-cluster",
							}.Build(),
						}.Build(),
					}.Build()
					return
				},
			})

			// Start the server:
			server.Start()

			// Use the helper to send the request, and verify the metadata is returned:
			objectHelper := helper.Lookup("cluster")
			Expect(objectHelper).ToNot(BeNil())
			object, err := objectHelper.Get(ctx, "123")
			Expect(err).ToNot(HaveOccurred())
			Expect(object).ToNot(BeNil())
			metadata := objectHelper.GetMetadata(object)
			Expect(metadata).ToNot(BeNil())
			Expect(metadata.GetName()).To(Equal("my-cluster"))
		})

		It("Sorts types according to package order", func() {
			// Create a helper with multiple packages, where private has a lower order (0) than public (1),
			// so private types should appear first:
			multiPackageHelper, err := NewHelper().
				SetLogger(logger).
				SetConnection(connection).
				AddPackage(packages.PrivateV1, 0).
				AddPackage(packages.PublicV1, 1).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Get all the type names:
			names := multiPackageHelper.Names()

			// Verify that private types come before public types:
			var lastPrivateIndex int = -1
			var firstPublicIndex int = -1
			for i, name := range names {
				if strings.HasPrefix(name, packages.PrivateV1) {
					lastPrivateIndex = i
				}
				if strings.HasPrefix(name, packages.PublicV1) && firstPublicIndex == -1 {
					firstPublicIndex = i
				}
			}

			// If both package types exist, verify that all private types come before public types:
			if lastPrivateIndex >= 0 && firstPublicIndex >= 0 {
				Expect(lastPrivateIndex).To(
					BeNumerically("<", firstPublicIndex),
					"All private types should come before public types",
				)
			}

			// Verify that within each package, types are sorted alphabetically:
			privateTypes := []string{}
			publicTypes := []string{}
			for _, name := range names {
				if strings.HasPrefix(name, packages.PrivateV1) {
					privateTypes = append(privateTypes, name)
				}
				if strings.HasPrefix(name, packages.PublicV1) {
					publicTypes = append(publicTypes, name)
				}
			}

			// Check that private types are sorted alphabetically:
			if len(privateTypes) > 1 {
				for i := 1; i < len(privateTypes); i++ {
					Expect(privateTypes[i-1] < privateTypes[i]).To(
						BeTrue(),
						"Types within private package should be sorted alphabetically, '%s' "+
							"should come before '%s'",
						privateTypes[i-1], privateTypes[i],
					)
				}
			}

			// Check that public types are sorted alphabetically:
			if len(publicTypes) > 1 {
				for i := 1; i < len(publicTypes); i++ {
					Expect(publicTypes[i-1] < publicTypes[i]).To(
						BeTrue(),
						"Types within public package should be sorted alphabetically, '%s' "+
							"should come before '%s'",
						publicTypes[i-1], publicTypes[i],
					)
				}
			}
		})

		It("Sorts types according to package order when adding packages", func() {
			// Create a helper using AddPackages method with reversed order:
			multiPackageHelper, err := NewHelper().
				SetLogger(logger).
				SetConnection(connection).
				AddPackages(map[string]int{
					packages.PublicV1:  2,
					packages.PrivateV1: 1,
				}).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Get all the type names:
			names := multiPackageHelper.Names()

			// Verify that private types come before public types:
			var lastPrivateIndex int = -1
			var firstPublicIndex int = -1
			for i, name := range names {
				if strings.HasPrefix(name, packages.PrivateV1) {
					lastPrivateIndex = i
				}
				if strings.HasPrefix(name, packages.PublicV1) && firstPublicIndex == -1 {
					firstPublicIndex = i
				}
			}

			// If both package types exist, verify that all private types come before public types:
			if lastPrivateIndex >= 0 && firstPublicIndex >= 0 {
				Expect(lastPrivateIndex).To(
					BeNumerically("<", firstPublicIndex),
					"All private types should come before public types when "+
						"private package has lower order",
				)
			}
		})
	})
})
