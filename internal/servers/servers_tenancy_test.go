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
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	ffv1 "github.com/osac-project/fulfillment-service/internal/api/fulfillment/v1"
	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	sharedv1 "github.com/osac-project/fulfillment-service/internal/api/shared/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Tenancy logic", func() {
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
		db := server.MakeDatabase()
		DeferCleanup(db.Close)
		pool, err := pgxpool.New(ctx, db.MakeURL())
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
			err := tm.End(ctx, tx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)

		// Create the tables:
		err = dao.CreateTables(
			ctx,
			"cluster_templates",
			"clusters",
		)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Returns tenants in metadata when object is created", func() {
		// Create a mock tenancy logic that returns specific tenants:
		tenancy := auth.NewMockTenancyLogic(ctrl)
		tenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
			Return(
				collections.NewSet("my-tenant", "your-tenant"),
				nil,
			).
			AnyTimes()
		tenancy.EXPECT().DetermineDefaultTenants(gomock.Any()).
			Return(
				collections.NewSet("my-tenant", "your-tenant"),
				nil,
			).
			AnyTimes()
		tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(
				collections.NewSet("my-tenant", "your-tenant"),
				nil,
			).
			AnyTimes()

		// Create the template using the DAO directly (this is setup for the test):
		templatesDao, err := dao.NewGenericDAO[*privatev1.ClusterTemplate]().
			SetLogger(logger).
			SetTable("cluster_templates").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		_, err = templatesDao.Create().SetObject(privatev1.ClusterTemplate_builder{
			Id:          "my-template",
			Title:       "My template",
			Description: "My template",
		}.Build()).Do(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create the public clusters server that uses the tenancy logic:
		clustersServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create a cluster using the public server to verify tenant assignment:
		response, err := clustersServer.Create(ctx, ffv1.ClustersCreateRequest_builder{
			Object: ffv1.Cluster_builder{
				Spec: ffv1.ClusterSpec_builder{
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())

		// Verify that the cluster metadata contains the expected tenants:
		cluster := response.GetObject()
		Expect(cluster).ToNot(BeNil())
		metadata := cluster.GetMetadata()
		Expect(metadata).ToNot(BeNil())
		tenants := metadata.GetTenants()
		Expect(tenants).To(ConsistOf(
			"my-tenant",
			"your-tenant",
		))
	})

	It("Rejects object creation when assigned tenants are empty", func() {
		// Create a tenancy logic that returns at least one tenant for setup:
		setupTenancy := auth.NewMockTenancyLogic(ctrl)
		setupTenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
			Return(collections.NewSet("setup-tenant"), nil).
			AnyTimes()
		setupTenancy.EXPECT().DetermineDefaultTenants(gomock.Any()).
			Return(collections.NewSet("setup-tenant"), nil).
			AnyTimes()
		setupTenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(collections.NewSet("setup-tenant"), nil).
			AnyTimes()

		// Create the template using the DAO with setup tenancy:
		templatesDao, err := dao.NewGenericDAO[*privatev1.ClusterTemplate]().
			SetLogger(logger).
			SetTable("cluster_templates").
			SetAttributionLogic(attribution).
			SetTenancyLogic(setupTenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		_, err = templatesDao.Create().SetObject(privatev1.ClusterTemplate_builder{
			Id:          "my-template",
			Title:       "My template",
			Description: "My template",
		}.Build()).Do(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create a tenancy logic that returns empty tenants:
		emptyTenancy := auth.NewMockTenancyLogic(ctrl)
		emptyTenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
			Return(collections.NewSet[string](), nil).
			AnyTimes()
		emptyTenancy.EXPECT().DetermineDefaultTenants(gomock.Any()).
			Return(collections.NewSet[string](), nil).
			AnyTimes()
		emptyTenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(collections.NewSet[string](), nil).
			AnyTimes()

		// Create the clusters server with the empty tenancy logic:
		clustersServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(emptyTenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Attempt to create a cluster and verify it fails:
		response, err := clustersServer.Create(ctx, ffv1.ClustersCreateRequest_builder{
			Object: ffv1.Cluster_builder{
				Spec: ffv1.ClusterSpec_builder{
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

	It("Uses default tenants when tenants are explicitly empty", func() {
		// Create a tenancy logic that returns valid tenants:
		tenant := collections.NewSet("my-tenant")
		tenancy := auth.NewMockTenancyLogic(ctrl)
		tenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
			Return(tenant, nil).
			AnyTimes()
		tenancy.EXPECT().DetermineDefaultTenants(gomock.Any()).
			Return(tenant, nil).
			AnyTimes()
		tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(tenant, nil).
			AnyTimes()

		// Create the template using the DAO:
		templatesDao, err := dao.NewGenericDAO[*privatev1.ClusterTemplate]().
			SetLogger(logger).
			SetTable("cluster_templates").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		_, err = templatesDao.Create().SetObject(privatev1.ClusterTemplate_builder{
			Id:          "my-template",
			Title:       "My template",
			Description: "My template",
		}.Build()).Do(ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create the clusters server:
		clustersServer, err := NewClustersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Attempt to create a cluster with explicitly empty tenants and verify it fails:
		response, err := clustersServer.Create(ctx, ffv1.ClustersCreateRequest_builder{
			Object: ffv1.Cluster_builder{
				Metadata: sharedv1.Metadata_builder{
					Tenants: []string{},
				}.Build(),
				Spec: ffv1.ClusterSpec_builder{
					Template: "my-template",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Verify that the cluster metadata contains the expected tenants:
		cluster := response.GetObject()
		Expect(cluster).ToNot(BeNil())
		tenants := cluster.GetMetadata().GetTenants()
		Expect(tenants).To(ConsistOf("my-tenant"))
	})
})
