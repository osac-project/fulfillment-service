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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Project visibility", func() {
	var (
		ctrl *gomock.Controller
		ctx  context.Context
		tx   database.Tx
	)

	BeforeEach(func() {
		var err error

		// Create the mock controller:
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)

		// Create a context:
		ctx = context.Background()

		// Prepare the database pool:
		db, err := server.NewInstance().Build()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(db.Close)
		pool, err := db.Pool(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)

		// Create the transaction manager:
		tm, err := database.NewTxManager().
			SetLogger(logger).
			SetPool(pool).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Start a transaction and add it to the context:
		tx, err = tm.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := tx.End(ctx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)

		// Create the tenants used in the tests:
		tenantsDao, err := dao.NewGenericDAO[*privatev1.Organization]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		createTenant := func(name string) {
			_, err = tenantsDao.Create().
				SetObject(privatev1.Organization_builder{
					Id: name,
					Metadata: privatev1.Metadata_builder{
						Name:   name,
						Tenant: name,
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
		}
		createTenant("my-tenant")

		// Create the projects used in the tests:
		projectsDao, err := dao.NewGenericDAO[*privatev1.Project]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		createProject := func(tenant, name string) {
			_, err = projectsDao.Create().
				SetObject(privatev1.Project_builder{
					Metadata: privatev1.Metadata_builder{
						Tenant: tenant,
						Name:   name,
					}.Build(),
				}.Build()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
		}
		createProject("my-tenant", "my-project")
		createProject("my-tenant", "project-a")
		createProject("my-tenant", "project-b")
	})

	// newTenancy creates a mock tenancy logic with the given visible projects and default project. Tenant
	// visibility is always universal.
	newTenancy := func(visibleProjects collections.Set[string], defaultProject string) *auth.MockTenancyLogic {
		tenancy := auth.NewMockTenancyLogic(ctrl)
		tenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
			Return(auth.AllTenants, nil).
			AnyTimes()
		tenancy.EXPECT().DetermineDefaultTenant(gomock.Any()).
			Return("my-tenant", nil).
			AnyTimes()
		tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(auth.AllTenants, nil).
			AnyTimes()
		tenancy.EXPECT().DetermineAssignableProjects(gomock.Any()).
			Return(visibleProjects, nil).
			AnyTimes()
		tenancy.EXPECT().DetermineDefaultProject(gomock.Any()).
			Return(defaultProject, nil).
			AnyTimes()
		tenancy.EXPECT().DetermineVisibleProjects(gomock.Any()).
			Return(visibleProjects, nil).
			AnyTimes()
		return tenancy
	}

	// createTemplate creates a cluster template using the DAO directly.
	createTemplate := func(tenancy auth.TenancyLogic) {
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
					Tenant:  "my-tenant",
					Project: auth.DefaultProject,
				}.Build(),
			}.Build()).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
	}

	It("Assigns default project when object is created", func() {
		tenancy := newTenancy(auth.AllProjects, "my-project")
		createTemplate(tenancy)

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
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.GetObject().GetMetadata().GetProject()).To(Equal("my-project"))
	})

	It("Returns objects only from visible projects when listing", func() {
		// Create two clusters in different projects using a DAO with universal visibility:
		adminTenancy := newTenancy(collections.NewUniversalSet[string](), "project-a")
		createTemplate(adminTenancy)

		clustersServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(adminTenancy).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		_, err = clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
			Object: publicv1.Cluster_builder{
				Spec: publicv1.ClusterSpec_builder{
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Create a second cluster in a different project:
		adminTenancyB := newTenancy(collections.NewUniversalSet[string](), "project-b")
		clustersServerB, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(adminTenancyB).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		_, err = clustersServerB.Create(ctx, publicv1.ClustersCreateRequest_builder{
			Object: publicv1.Cluster_builder{
				Spec: publicv1.ClusterSpec_builder{
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// List with a user that can only see project-a:
		restrictedTenancy := newTenancy(collections.NewSet("project-a"), "project-a")
		restrictedServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(restrictedTenancy).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		listResponse, err := restrictedServer.List(ctx, publicv1.ClustersListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(listResponse.GetItems()).To(HaveLen(1))
		Expect(listResponse.GetItems()[0].GetMetadata().GetProject()).To(Equal("project-a"))
	})

	It("Returns not found when getting an object from an invisible project", func() {
		// Create a cluster in project-a:
		adminTenancy := newTenancy(collections.NewUniversalSet[string](), "project-a")
		createTemplate(adminTenancy)

		clustersServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(adminTenancy).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		createResponse, err := clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
			Object: publicv1.Cluster_builder{
				Spec: publicv1.ClusterSpec_builder{
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		clusterID := createResponse.GetObject().GetId()

		// Try to get the cluster with a user that can only see project-b:
		restrictedTenancy := newTenancy(collections.NewSet("project-b"), "project-b")
		restrictedServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(restrictedTenancy).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		_, err = restrictedServer.Get(ctx, publicv1.ClustersGetRequest_builder{
			Id: clusterID,
		}.Build())
		Expect(err).To(HaveOccurred())
	})

	It("Honors client-provided project when it is assignable", func() {
		// Use a tenancy that includes both the template's project and the user's projects so the template
		// lookup succeeds:
		tenancy := newTenancy(
			collections.NewSet(auth.DefaultProject, "project-a", "project-b"),
			"project-a",
		)
		createTemplate(tenancy)

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
					Project: "project-b",
				}.Build(),
				Spec: publicv1.ClusterSpec_builder{
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response.GetObject().GetMetadata().GetProject()).To(Equal("project-b"))
	})

	It("Rejects creation when client-provided project is not assignable", func() {
		// Use a tenancy that includes the template's project so the lookup succeeds, but the user can
		// only assign project-a:
		tenancy := newTenancy(
			collections.NewSet(auth.DefaultProject, "project-a"),
			"project-a",
		)
		createTemplate(tenancy)

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
					Project: "project-x",
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
		Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
		Expect(status.Message()).To(ContainSubstring("project-x"))
	})

	It("Rejects changing project on update", func() {
		tenancy := newTenancy(
			collections.NewSet(auth.DefaultProject, "project-a", "project-b"),
			"project-a",
		)
		createTemplate(tenancy)

		clustersServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		createResponse, err := clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
			Object: publicv1.Cluster_builder{
				Metadata: publicv1.Metadata_builder{
					Project: "project-a",
				}.Build(),
				Spec: publicv1.ClusterSpec_builder{
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()

		_, err = clustersServer.Update(ctx, publicv1.ClustersUpdateRequest_builder{
			Object: publicv1.Cluster_builder{
				Id: object.GetId(),
				Metadata: publicv1.Metadata_builder{
					Project: "project-b",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(ContainSubstring("immutable"))
	})

	It("Preserves project when update does not specify it", func() {
		tenancy := newTenancy(collections.NewUniversalSet[string](), "my-project")
		createTemplate(tenancy)

		clustersServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetScheme(testScheme).
			Build()
		Expect(err).ToNot(HaveOccurred())

		createResponse, err := clustersServer.Create(ctx, publicv1.ClustersCreateRequest_builder{
			Object: publicv1.Cluster_builder{
				Metadata: publicv1.Metadata_builder{
					Project: "my-project",
				}.Build(),
				Spec: publicv1.ClusterSpec_builder{
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()

		updateResponse, err := clustersServer.Update(ctx, publicv1.ClustersUpdateRequest_builder{
			Object: publicv1.Cluster_builder{
				Id: object.GetId(),
				Spec: publicv1.ClusterSpec_builder{
					NodeSets: map[string]*publicv1.ClusterNodeSet{
						"compute": publicv1.ClusterNodeSet_builder{
							HostType: "acme_1tib",
							Size:     4,
						}.Build(),
					},
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResponse.GetObject().GetMetadata().GetProject()).To(Equal("my-project"))
	})
})
