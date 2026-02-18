package it

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/osac-project/fulfillment-common/testing"
	ffv1 "github.com/osac-project/fulfillment-service/internal/api/fulfillment/v1"
	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Multitenancy authentication error handling", Label("multitenancy", "autherrors"), func() {
	DescribeTable(
		"returns error when authenticating with invalid token",
		func(testUrl string) {
			invalidToken := testing.MakeTokenString("test", 0)

			req, err := http.NewRequest("GET", testUrl, nil)
			Expect(err).ToNot(HaveOccurred())

			// Set invalid token in request header
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", invalidToken))

			// Make request with invalid token
			resp, err := http.Get(testUrl)
			Expect(err).ToNot(HaveOccurred())

			// Check that unauthorized status code is returned
			Eventually(func(g Gomega) {
				resp, err = http.Get(testUrl)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(resp).ToNot(BeNil())
				Expect(resp.StatusCode).To(BeNumerically(">=", http.StatusUnauthorized))
				g.Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			}).WithTimeout(100 * time.Millisecond).WithPolling(10 * time.Millisecond).Should(Succeed())

			defer resp.Body.Close()

			// Check that permission denied error is returned in response body
			body, err := io.ReadAll(resp.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(body).To(ContainSubstring("permission denied"))
		},
		Entry("clusters", fmt.Sprintf("https://%s/api/fulfillment/v1/clusters", serviceAddr)),
		Entry("cluster templates", fmt.Sprintf("https://%s/api/fulfillment/v1/cluster_templates", serviceAddr)),
		Entry("host pools", fmt.Sprintf("https://%s/api/fulfillment/v1/host_pools", serviceAddr)),
		Entry("host classes", fmt.Sprintf("https://%s/api/fulfillment/v1/host_classes", serviceAddr)),
	)

	DescribeTable(
		"returns error when user is not authenticated",
		func(testUrl string) {
			var testUser string
			var testTenant string

			// Get a random user and tenant from map of users
			for user, tenant := range ServiceAccountTenants {
				testUser = user
				testTenant = tenant
				break
			}

			// Check that requests can be made with valid token
			tokenSource, err := tool.makeKubernetesTokenSource(context.Background(), testUser, testTenant)
			Expect(err).ToNot(HaveOccurred())

			req, err := http.NewRequest("GET", testUrl, nil)
			Expect(err).ToNot(HaveOccurred())

			// Set token in request header
			token, err := tokenSource.Token(context.Background())
			Expect(err).ToNot(HaveOccurred())
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token.Access))

			// Make request with valid token
			Eventually(func(g Gomega) {
				resp, err := http.DefaultClient.Do(req)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
			}).WithTimeout(100 * time.Millisecond).WithPolling(10 * time.Millisecond).Should(Succeed())

			// Make request with invalid token
			var resp *http.Response

			// Check that unauthorized status code is returned
			Eventually(func(g Gomega) {
				resp, err = http.Get(testUrl)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(resp).ToNot(BeNil())
				Expect(resp.StatusCode).To(BeNumerically(">=", http.StatusUnauthorized))
				g.Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			}).WithTimeout(100 * time.Millisecond).WithPolling(10 * time.Millisecond).Should(Succeed())

			defer resp.Body.Close()

			// Check that permission denied error is returned in response body
			body, err := io.ReadAll(resp.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(body).To(ContainSubstring("permission denied"))
		},
		Entry("clusters", fmt.Sprintf("https://%s/api/fulfillment/v1/clusters", serviceAddr)),
		Entry("cluster templates", fmt.Sprintf("https://%s/api/fulfillment/v1/cluster_templates", serviceAddr)),
		Entry("host pools", fmt.Sprintf("https://%s/api/fulfillment/v1/host_pools", serviceAddr)),
		Entry("host classes", fmt.Sprintf("https://%s/api/fulfillment/v1/host_classes", serviceAddr)),
	)
})

var _ = Describe("Multitenancy basic tenant isolation", Ordered, Label("multitenancy", "isolation"), func() {
	Describe("serviceaccount tenants", func() {
		var (
			tenantUserMapping map[string][]string
			ctx               context.Context
		)

		BeforeAll(func() {
			ctx = context.Background()

			// Create map to track which users belong to which tenants
			tenantUserMapping = make(map[string][]string)

			Expect(len(ServiceAccountTenants)).To(BeNumerically(">", 1))

			// Populate map to track which users belong to which tenants
			for user, tenant := range ServiceAccountTenants {
				tenantUserMapping[tenant] = append(tenantUserMapping[tenant], user)
			}
		})

		Describe("cluster resources", func() {
			var (
				tenantClusterMapping map[string][]string
			)

			BeforeAll(func() {
				// Create map to track which clusters belong to which tenants
				tenantClusterMapping = make(map[string][]string)

				// Create host class for testing
				hostClassesClient := privatev1.NewHostClassesClient(tool.AdminConn())
				hostClassId := "basic-sa-isolation-hostclass"
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

				// Create cluster template for testing
				templateId := "basic-sa-isolation-template"
				templatesClient := privatev1.NewClusterTemplatesClient(tool.adminConn)
				_, err = templatesClient.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
					Object: privatev1.ClusterTemplate_builder{
						Id: templateId,
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

				// Create a cluster object for each user
				for user, tenant := range ServiceAccountTenants {
					tokenSource, err := tool.makeKubernetesTokenSource(ctx, user, tenant)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					clustersClient := ffv1.NewClustersClient(conn)
					createResponse, err := clustersClient.Create(ctx, ffv1.ClustersCreateRequest_builder{
						Object: ffv1.Cluster_builder{
							Spec: ffv1.ClusterSpec_builder{
								Template: templateId,
							}.Build(),
						}.Build(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())
					clusterObject := createResponse.GetObject()

					DeferCleanup(func() { deleteCluster(ctx, clusterObject.GetId()) })

					// Populate map to track which clusters belong to which tenants
					tenantClusterMapping[tenant] = append(tenantClusterMapping[tenant], clusterObject.GetId())
				}
			})

			It("shared within the same tenant", func() {
				// List clusters for each user
				for user, tenant := range ServiceAccountTenants {
					tokenSource, err := tool.makeKubernetesTokenSource(ctx, user, tenant)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					response := listClusters(ctx, conn)
					Expect(response).ToNot(BeNil())
					Expect(len(response.Items)).To(Equal(len(tenantUserMapping[tenant])))

					// Check that each cluster appears under expected tenant
					for _, cluster := range response.GetItems() {
						Expect(tenantClusterMapping[tenant]).To(ContainElement(cluster.GetId()))
					}
				}
			})

			It("isolated between tenants", func() {
				// List clusters for each user
				for user, tenant := range ServiceAccountTenants {
					tokenSource, err := tool.makeKubernetesTokenSource(ctx, user, tenant)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					response := listClusters(ctx, conn)
					Expect(response).ToNot(BeNil())
					Expect(len(response.Items)).To(Equal(len(tenantUserMapping[tenant])))

					// Check that each cluster is isolated between tenants
					for _, cluster := range response.GetItems() {
						for _, otherTenant := range ServiceAccountTenants {
							if otherTenant != tenant {
								Expect(tenantClusterMapping[otherTenant]).ToNot(ContainElement(cluster.GetId()))
							}
						}
					}
				}
			})

			It("assigned the correct tenant after creation", func() {
				for tenant, clusters := range tenantClusterMapping {
					for _, cluster := range clusters {
						clustersClient := privatev1.NewClustersClient(tool.AdminConn())

						clusterResponse, err := clustersClient.Get(ctx, privatev1.ClustersGetRequest_builder{
							Id: cluster,
						}.Build())
						Expect(err).ToNot(HaveOccurred())
						Expect(len(clusterResponse.GetObject().Metadata.Tenants)).To(Equal(1))
						Expect(clusterResponse.GetObject().Metadata.Tenants[0]).To(Equal(tenant))
					}
				}
			})

			DescribeTable(
				"cross-tenant",
				func(operation func(client ffv1.ClustersClient, clusterID string) error) {
					for clusterTenant, clusters := range tenantClusterMapping {
						for user, userTenant := range ServiceAccountTenants {
							// Skip if cluster is owned by the same tenant
							if clusterTenant == userTenant {
								continue
							}

							tokenSource, err := tool.makeKubernetesTokenSource(ctx, user, userTenant)
							Expect(err).ToNot(HaveOccurred())
							conn, err := tool.makeGrpcConn(tokenSource)
							Expect(err).ToNot(HaveOccurred())

							clustersClient := ffv1.NewClustersClient(conn)

							for _, cluster := range clusters {
								err := operation(clustersClient, cluster)
								Expect(err).To(HaveOccurred())
								status, ok := grpcstatus.FromError(err)
								Expect(ok).To(BeTrue())
								Expect(status.Code()).To(Equal(grpccodes.NotFound))
							}
						}
					}

				},
				Entry("deletion is not allowed", func(client ffv1.ClustersClient, clusterID string) error {
					_, err := client.Delete(ctx, ffv1.ClustersDeleteRequest_builder{
						Id: clusterID,
					}.Build())

					return err
				}),
				Entry("update is not allowed", func(client ffv1.ClustersClient, clusterID string) error {
					_, err := client.Update(ctx, ffv1.ClustersUpdateRequest_builder{
						Object: ffv1.Cluster_builder{
							Id: clusterID,
							Spec: ffv1.ClusterSpec_builder{
								Template: "cross-tenant-update-template",
							}.Build(),
						}.Build(),
					}.Build())

					return err
				}),
			)
		})

		Describe("host pool resources", func() {
			var (
				tenantHostPoolMapping map[string][]string
			)

			BeforeAll(func() {
				// Create map to track which host pools belong to which tenants
				tenantHostPoolMapping = make(map[string][]string)

				// Create a host pool for each user
				for user, tenant := range ServiceAccountTenants {
					tokenSource, err := tool.makeKubernetesTokenSource(ctx, user, tenant)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					hostPoolsClient := ffv1.NewHostPoolsClient(conn)
					createResponse, err := hostPoolsClient.Create(ctx, ffv1.HostPoolsCreateRequest_builder{
						Object: ffv1.HostPool_builder{
							Spec: ffv1.HostPoolSpec_builder{
								HostSets: map[string]*ffv1.HostPoolHostSet{
									"my-host-set": ffv1.HostPoolHostSet_builder{
										HostClass: "blah",
										Size:      3,
									}.Build(),
								},
							}.Build(),
						}.Build(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())
					hostPoolObject := createResponse.GetObject()
					DeferCleanup(func() { deleteHostPool(ctx, hostPoolObject.GetId()) })

					// Populate map to track which host pools belong to which tenants
					tenantHostPoolMapping[tenant] = append(tenantHostPoolMapping[tenant], hostPoolObject.GetId())
				}
			})

			It("shared within the same tenant", func() {
				// List host pools for each user
				for user, tenant := range ServiceAccountTenants {
					tokenSource, err := tool.makeKubernetesTokenSource(ctx, user, tenant)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					response := listHostPools(ctx, conn)
					Expect(response).ToNot(BeNil())
					Expect(len(response.Items)).To(Equal(len(tenantUserMapping[tenant])))

					// Check that each host pool appears under expected tenant
					for _, hostPool := range response.GetItems() {
						Expect(tenantHostPoolMapping[tenant]).To(ContainElement(hostPool.GetId()))
					}
				}
			})

			It("isolated between tenants", func() {
				// List host pools for each user
				for user, tenant := range ServiceAccountTenants {
					tokenSource, err := tool.makeKubernetesTokenSource(ctx, user, tenant)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					response := listHostPools(ctx, conn)
					Expect(response).ToNot(BeNil())
					Expect(len(response.Items)).To(Equal(len(tenantUserMapping[tenant])))

					// Check that each host pool is isolated between tenants
					for _, hostPool := range response.GetItems() {
						for _, otherTenant := range ServiceAccountTenants {
							if otherTenant != tenant {
								Expect(tenantHostPoolMapping[otherTenant]).ToNot(ContainElement(hostPool.GetId()))
							}
						}
					}
				}
			})

			It("assigned the correct tenant after creation", func() {
				for tenant, hostPools := range tenantHostPoolMapping {
					for _, hostPool := range hostPools {
						hostPoolsClient := privatev1.NewHostPoolsClient(tool.AdminConn())

						hostPoolResponse, err := hostPoolsClient.Get(ctx, privatev1.HostPoolsGetRequest_builder{
							Id: hostPool,
						}.Build())
						Expect(err).ToNot(HaveOccurred())
						Expect(len(hostPoolResponse.GetObject().Metadata.Tenants)).To(Equal(1))
						Expect(hostPoolResponse.GetObject().Metadata.Tenants[0]).To(Equal(tenant))
					}
				}
			})

			DescribeTable(
				"cross-tenant",
				func(operation func(client ffv1.HostPoolsClient, hostPoolID string) error) {
					for hostPoolTenant, hostPools := range tenantHostPoolMapping {
						for user, userTenant := range ServiceAccountTenants {
							// Skip if host pool is owned by the same tenant
							if hostPoolTenant == userTenant {
								continue
							}

							tokenSource, err := tool.makeKubernetesTokenSource(ctx, user, userTenant)
							Expect(err).ToNot(HaveOccurred())
							conn, err := tool.makeGrpcConn(tokenSource)
							Expect(err).ToNot(HaveOccurred())

							hostPoolsClient := ffv1.NewHostPoolsClient(conn)

							for _, hostPool := range hostPools {
								err := operation(hostPoolsClient, hostPool)
								Expect(err).To(HaveOccurred())
								status, ok := grpcstatus.FromError(err)
								Expect(ok).To(BeTrue())
								Expect(status.Code()).To(Equal(grpccodes.NotFound))
							}
						}
					}

				},
				Entry("deletion is not allowed", func(client ffv1.HostPoolsClient, hostPoolID string) error {
					_, err := client.Delete(ctx, ffv1.HostPoolsDeleteRequest_builder{
						Id: hostPoolID,
					}.Build())

					return err
				}),
				Entry("update is not allowed", func(client ffv1.HostPoolsClient, hostPoolID string) error {
					_, err := client.Update(ctx, ffv1.HostPoolsUpdateRequest_builder{
						Object: ffv1.HostPool_builder{
							Id: hostPoolID,
							Spec: ffv1.HostPoolSpec_builder{
								HostSets: map[string]*ffv1.HostPoolHostSet{
									"my-host-set": ffv1.HostPoolHostSet_builder{
										HostClass: "cross-tenant-update-host-class",
										Size:      3,
									}.Build(),
								},
							}.Build(),
						}.Build(),
					}.Build())

					return err
				}),
			)
		})
	})

	Describe("OIDC tenants", func() {
		var (
			tenantUserMapping map[string][]string
			ctx               context.Context
		)

		BeforeAll(func() {
			ctx = context.Background()

			// Create map to track which users belong to which tenants
			tenantUserMapping = make(map[string][]string)

			Expect(len(OIDCTenants)).To(BeNumerically(">", 1))

			// Populate map to track which users belong to which tenants
			for user, tenants := range OIDCTenants {
				for _, tenant := range tenants {
					tenantUserMapping[tenant] = append(tenantUserMapping[tenant], user)
				}
			}
		})

		Describe("cluster resources", func() {
			var (
				tenantClusterMapping map[string][]string
				clusterTenantMapping map[string][]string
			)

			BeforeAll(func() {
				// Create map to track which clusters belong to which tenants
				tenantClusterMapping = make(map[string][]string)

				// Create map to track which clusters can be seen by which tenants
				clusterTenantMapping = make(map[string][]string)

				// Create host class for testing
				hostClassId := "basic-oidc-isolation-hostclass"
				hostClassesClient := privatev1.NewHostClassesClient(tool.adminConn)
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

				// Create cluster template for testing
				templateId := "basic-oidc-isolation-template"
				templatesClient := privatev1.NewClusterTemplatesClient(tool.adminConn)
				_, err = templatesClient.Create(ctx, privatev1.ClusterTemplatesCreateRequest_builder{
					Object: privatev1.ClusterTemplate_builder{
						Id: templateId,
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

				// Create a cluster object for each user
				for user, tenants := range OIDCTenants {
					tokenSource, err := tool.makeKeycloakTokenSource(ctx, user, usersPassword)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					clustersClient := ffv1.NewClustersClient(conn)
					createResponse, err := clustersClient.Create(ctx, ffv1.ClustersCreateRequest_builder{
						Object: ffv1.Cluster_builder{
							Spec: ffv1.ClusterSpec_builder{
								Template: templateId,
							}.Build(),
						}.Build(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())
					clusterObject := createResponse.GetObject()

					DeferCleanup(func() { deleteCluster(ctx, clusterObject.GetId()) })

					// Populate map to track which clusters belong to which tenants
					for _, tenant := range tenants {
						tenantClusterMapping[tenant] = append(tenantClusterMapping[tenant], clusterObject.GetId())
						clusterTenantMapping[clusterObject.GetId()] = append(clusterTenantMapping[clusterObject.GetId()], tenant)
					}
				}
			})
			It("shared within the same tenant", func() {
				// List clusters for each user
				for user, tenants := range OIDCTenants {
					tokenSource, err := tool.makeKeycloakTokenSource(ctx, user, usersPassword)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					response := listClusters(ctx, conn)
					Expect(response).ToNot(BeNil())

					Expect(len(response.Items)).To(Equal(calculateResponseSize(tenantUserMapping, tenants)))

					// Check that each cluster appears under expected tenant
					for _, cluster := range response.GetItems() {
						Expect(checkTenantMembership(tenantClusterMapping, tenants, cluster.GetId())).To(BeTrue())
					}
				}
			})

			It("isolated between tenants", func() {
				// List clusters for each user
				for user, tenants := range OIDCTenants {
					tokenSource, err := tool.makeKeycloakTokenSource(ctx, user, usersPassword)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					response := listClusters(ctx, conn)
					Expect(response).ToNot(BeNil())
					Expect(len(response.Items)).To(Equal(calculateResponseSize(tenantUserMapping, tenants)))

					// Check that each cluster is isolated between tenants
					for _, cluster := range response.GetItems() {
						clusterId := cluster.GetId()
						expectedTenants := clusterTenantMapping[clusterId]

						// For each tenant, verify correct isolation
						for tenant, tenantClusters := range tenantClusterMapping {
							if slices.Contains(expectedTenants, tenant) {
								// Tenant should have access - verify cluster is in their list
								Expect(tenantClusters).To(ContainElement(clusterId))
							} else {
								// Tenant should NOT have access - verify cluster is isolated
								Expect(tenantClusters).ToNot(ContainElement(clusterId))
							}
						}
					}
				}
			})
		})

		Describe("host pool resources", func() {
			var (
				tenantHostPoolMapping map[string][]string
				hostPoolTenantMapping map[string][]string
			)

			BeforeAll(func() {
				// Create map to track which host pools belong to which tenants
				tenantHostPoolMapping = make(map[string][]string)

				// Create map to track which host pools can be seen by which tenants
				hostPoolTenantMapping = make(map[string][]string)

				// Create a host pool for each user
				for user, tenants := range OIDCTenants {
					tokenSource, err := tool.makeKeycloakTokenSource(ctx, user, usersPassword)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					hostPoolsClient := ffv1.NewHostPoolsClient(conn)
					createResponse, err := hostPoolsClient.Create(ctx, ffv1.HostPoolsCreateRequest_builder{
						Object: ffv1.HostPool_builder{
							Spec: ffv1.HostPoolSpec_builder{
								HostSets: map[string]*ffv1.HostPoolHostSet{
									"my-host-set": ffv1.HostPoolHostSet_builder{
										HostClass: "blah",
										Size:      3,
									}.Build(),
								},
							}.Build(),
						}.Build(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())
					hostPoolObject := createResponse.GetObject()
					DeferCleanup(func() { deleteHostPool(ctx, hostPoolObject.GetId()) })

					// Populate map to track which host pools belong to which tenants
					for _, tenant := range tenants {
						tenantHostPoolMapping[tenant] = append(tenantHostPoolMapping[tenant], hostPoolObject.GetId())
						hostPoolTenantMapping[hostPoolObject.GetId()] = append(hostPoolTenantMapping[hostPoolObject.GetId()], tenant)
					}
				}
			})
			It("shared within the same tenant", func() {
				// List host pools for each user
				for user, tenants := range OIDCTenants {
					tokenSource, err := tool.makeKeycloakTokenSource(ctx, user, usersPassword)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					response := listHostPools(ctx, conn)
					Expect(response).ToNot(BeNil())

					Expect(len(response.Items)).To(Equal(calculateResponseSize(tenantUserMapping, tenants)))

					// Check that each host pool appears under expected tenant
					for _, hostPool := range response.GetItems() {
						Expect(checkTenantMembership(tenantHostPoolMapping, tenants, hostPool.GetId())).To(BeTrue())
					}
				}
			})

			It("isolated between tenants", func() {
				// List host pools for each user
				for user, tenants := range OIDCTenants {
					tokenSource, err := tool.makeKeycloakTokenSource(ctx, user, usersPassword)
					Expect(err).ToNot(HaveOccurred())
					conn, err := tool.makeGrpcConn(tokenSource)
					Expect(err).ToNot(HaveOccurred())

					response := listHostPools(ctx, conn)
					Expect(response).ToNot(BeNil())
					Expect(len(response.Items)).To(Equal(calculateResponseSize(tenantUserMapping, tenants)))

					// Check that each host pool is isolated between tenants
					for _, hostPool := range response.GetItems() {
						hostPoolId := hostPool.GetId()
						expectedTenants := hostPoolTenantMapping[hostPoolId]

						// For each tenant, verify correct isolation
						for tenant, tenantHostPools := range tenantHostPoolMapping {
							if slices.Contains(expectedTenants, tenant) {
								// Tenant should have access - verify host pool is in their list
								Expect(tenantHostPools).To(ContainElement(hostPoolId))
							} else {
								// Tenant should NOT have access - verify host pool is isolated
								Expect(tenantHostPools).ToNot(ContainElement(hostPoolId))
							}
						}
					}
				}
			})
		})
	})
})

func listClusters(ctx context.Context, conn *grpc.ClientConn) *ffv1.ClustersListResponse {
	clustersClient := ffv1.NewClustersClient(conn)
	response, err := clustersClient.List(ctx, ffv1.ClustersListRequest_builder{}.Build())
	Expect(err).ToNot(HaveOccurred(), "error listing clusters")

	return response
}

func listHostPools(ctx context.Context, conn *grpc.ClientConn) *ffv1.HostPoolsListResponse {
	hostPoolsClient := ffv1.NewHostPoolsClient(conn)
	response, err := hostPoolsClient.List(ctx, ffv1.HostPoolsListRequest_builder{}.Build())
	Expect(err).ToNot(HaveOccurred(), "error listing host pools")

	return response
}

func deleteCluster(ctx context.Context, clusterId string) error {
	clusterClient := privatev1.NewClustersClient(tool.AdminConn())
	_, err := clusterClient.Delete(ctx, privatev1.ClustersDeleteRequest_builder{
		Id: clusterId,
	}.Build())
	return err
}

func deleteHostPool(ctx context.Context, hostPoolId string) error {
	hostPoolClient := privatev1.NewHostPoolsClient(tool.AdminConn())
	_, err := hostPoolClient.Delete(ctx, privatev1.HostPoolsDeleteRequest_builder{
		Id: hostPoolId,
	}.Build())
	return err
}

// calculateResponseSize calculates the response size based on a tenant-resource mapping and a user's tenant membership
// The response size is the number of the distinct objects visible by all of the user's tenants
func calculateResponseSize(mapping map[string][]string, tenants []string) int {
	seenResources := make(map[string]bool)
	for _, tenant := range tenants {
		for _, resource := range mapping[tenant] {
			seenResources[resource] = true
		}
	}
	return len(seenResources)
}

// checkTenantMembership checks if a resource is visible to a user based on their tenant membership
// The resource is visible if it is seen by any of the user's tenants
func checkTenantMembership(tenantResourceMapping map[string][]string, tenants []string, resourceId string) bool {
	for _, tenant := range tenants {
		objects := tenantResourceMapping[tenant]
		if slices.Contains(objects, resourceId) {
			return true
		}
	}
	return false
}
