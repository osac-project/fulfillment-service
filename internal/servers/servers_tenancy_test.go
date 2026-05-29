/*
Copyright (c) 2025 Red Hat Inc.

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

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Tenancy logic", func() {
	BeforeEach(func() {
		createTenant(ctx, "my-tenant")
	})

	It("Returns tenant in metadata when object is created", func() {
		// Create a mock tenancy logic that returns a specific tenant:
		tenancy := auth.NewMockTenancyLogic(ctrl)
		tenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
			Return(
				collections.NewSet("my-tenant"),
				nil,
			).
			AnyTimes()
		tenancy.EXPECT().DetermineDefaultTenant(gomock.Any()).
			Return("my-tenant", nil).
			AnyTimes()
		tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(
				collections.NewSet("my-tenant"),
				nil,
			).
			AnyTimes()

		// Create the template using the DAO directly (this is setup for the test):
		templatesDao, err := dao.NewGenericDAO[*privatev1.ClusterTemplate]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		_, err = templatesDao.Create().
			SetObject(
				privatev1.ClusterTemplate_builder{
					Id:          "my-template",
					Title:       "My template",
					Description: "My template",
					Metadata: privatev1.Metadata_builder{
						Tenant: "my-tenant",
					}.Build(),
				}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create the public clusters server that uses the tenancy logic:
		clustersServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create a cluster using the public server to verify tenant assignment:
		response, err := clustersServer.Create(
			ctx,
			publicv1.ClustersCreateRequest_builder{
				Object: publicv1.Cluster_builder{
					Spec: publicv1.ClusterSpec_builder{
						Template: "my-template",
					}.Build(),
				}.Build(),
			}.Build(),
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())

		// Verify that the cluster metadata contains the expected tenant:
		cluster := response.GetObject()
		Expect(cluster).ToNot(BeNil())
		metadata := cluster.GetMetadata()
		Expect(metadata).ToNot(BeNil())
		tenant := metadata.GetTenant()
		Expect(tenant).To(Equal("my-tenant"))
	})

	It("Rejects object creation when assigned tenants are empty", func() {
		// Create the template using the DAO with setup tenancy:
		templatesDao, err := dao.NewGenericDAO[*privatev1.ClusterTemplate]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		_, err = templatesDao.Create().
			SetObject(privatev1.ClusterTemplate_builder{
				Id:          "my-template",
				Title:       "My template",
				Description: "My template",
				Metadata: privatev1.Metadata_builder{
					Tenant: "my-tenant",
				}.Build(),
			}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create a tenancy logic that doesn't return assignable tenants:
		tenancy := auth.NewMockTenancyLogic(ctrl)
		tenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
			Return(collections.NewSet[string](), nil).
			AnyTimes()
		tenancy.EXPECT().DetermineDefaultTenant(gomock.Any()).
			Return("my-tenant", nil).
			AnyTimes()
		tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(collections.NewSet("my-tenant"), nil).
			AnyTimes()

		// Create the clusters server with the empty tenancy logic:
		clustersServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Attempt to create a cluster and verify it fails:
		response, err := clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
			Object: publicv1.Cluster_builder{
				Spec: publicv1.ClusterSpec_builder{
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(response).To(BeNil())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
		Expect(status.Message()).To(Equal("there are no assignable tenants"))
	})

	It("Uses default tenant when tenant is explicitly empty", func() {
		// Create a tenancy logic that returns a valid tenant:
		tenancy := auth.NewMockTenancyLogic(ctrl)
		tenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
			Return(collections.NewSet("my-tenant"), nil).
			AnyTimes()
		tenancy.EXPECT().DetermineDefaultTenant(gomock.Any()).
			Return("my-tenant", nil).
			AnyTimes()
		tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(collections.NewSet("my-tenant"), nil).
			AnyTimes()

		// Create the template using the DAO:
		templatesDao, err := dao.NewGenericDAO[*privatev1.ClusterTemplate]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		_, err = templatesDao.Create().
			SetObject(
				privatev1.ClusterTemplate_builder{
					Id:          "my-template",
					Title:       "My template",
					Description: "My template",
					Metadata: privatev1.Metadata_builder{
						Tenant: "my-tenant",
					}.Build(),
				}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create the clusters server:
		clustersServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Attempt to create a cluster with explicitly empty tenant and verify it uses the default:
		response, err := clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
			Object: publicv1.Cluster_builder{
				Metadata: publicv1.Metadata_builder{
					Tenant: "",
				}.Build(),
				Spec: publicv1.ClusterSpec_builder{
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Verify that the cluster metadata contains the expected tenant:
		cluster := response.GetObject()
		Expect(cluster).ToNot(BeNil())
		tenant := cluster.GetMetadata().GetTenant()
		Expect(tenant).To(Equal("my-tenant"))
	})

	It("Rejects object creation when assigned tenant is invisible to the user", func() {
		// Create a tenancy logic that returns visible tenants:
		visible := collections.NewSet("my-tenant")
		tenancy := auth.NewMockTenancyLogic(ctrl)
		tenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
			Return(visible, nil).
			AnyTimes()
		tenancy.EXPECT().DetermineDefaultTenant(gomock.Any()).
			Return("my-tenant", nil).
			AnyTimes()
		tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(visible, nil).
			AnyTimes()

		// Create the template:
		templatesDao, err := dao.NewGenericDAO[*privatev1.ClusterTemplate]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		_, err = templatesDao.Create().
			SetObject(privatev1.ClusterTemplate_builder{
				Id: "our-template",
				Metadata: privatev1.Metadata_builder{
					Tenant: "my-tenant",
				}.Build(),
			}.Build(),
			).Do(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create the clusters server:
		clustersServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Attempt to create an object with a tenant that is invisible to the user and verify that it fails:
		response, err := clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
			Object: publicv1.Cluster_builder{
				Metadata: publicv1.Metadata_builder{
					Tenant: "your-tenant",
				}.Build(),
				Spec: publicv1.ClusterSpec_builder{
					Template: "our-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(response).To(BeNil())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
		Expect(status.Message()).To(Equal("tenant 'your-tenant' doesn't exist"))
	})

	It("Rejects object creation when tenant is visible to the user, but doesn't exist in the database", func() {
		// Create a tenancy logic that returns visible tenants:
		tenancy := auth.NewMockTenancyLogic(ctrl)
		tenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
			Return(auth.AllTenants, nil).
			AnyTimes()
		tenancy.EXPECT().DetermineDefaultTenant(gomock.Any()).
			Return(auth.SharedTenant, nil).
			AnyTimes()
		tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(auth.AllTenants, nil).
			AnyTimes()

		// Create the server:
		server, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create the template using the DAO:
		templatesDao, err := dao.NewGenericDAO[*privatev1.ClusterTemplate]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		_, err = templatesDao.Create().
			SetObject(
				privatev1.ClusterTemplate_builder{
					Id:          "my-template",
					Title:       "My template",
					Description: "My template",
					Metadata: privatev1.Metadata_builder{
						Tenant: "my-tenant",
					}.Build(),
				}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Attempt to create an object and verify that it fails:
		response, err := server.Create(ctx, publicv1.ClustersCreateRequest_builder{
			Object: publicv1.Cluster_builder{
				Metadata: publicv1.Metadata_builder{
					Name:   "my-cluster",
					Tenant: "does-not-exist",
				}.Build(),
				Spec: publicv1.ClusterSpec_builder{
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(response).To(BeNil())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(Equal("tenant 'does-not-exist' doesn't exist"))
	})
})
