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
	"log/slog"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	grpcmetadata "google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/logging"
)

func TestServers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Servers package")
}

var (
	ctx         context.Context
	ctrl        *gomock.Controller
	logger      *slog.Logger
	server      *database.Container
	tm          database.TxManager
	attribution *auth.MockAttributionLogic
	tenancy     *auth.MockTenancyLogic
)

var _ = BeforeSuite(func() {
	var err error

	// Create the mock controller:
	ctrl = gomock.NewController(GinkgoT())
	DeferCleanup(ctrl.Finish)

	// Create the logger:
	logger, err = logging.NewLogger().
		SetLevel(slog.LevelDebug.String()).
		SetWriter(GinkgoWriter).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Create the attribution logic:
	attribution = auth.NewMockAttributionLogic(ctrl)
	attribution.EXPECT().DetermineAssignedCreator(gomock.Any()).
		Return("system", nil).
		AnyTimes()

	// Create the tenancy logic:
	tenancy = auth.NewMockTenancyLogic(ctrl)
	tenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
		Return(auth.AllTenants, nil).
		AnyTimes()
	tenancy.EXPECT().DetermineDefaultTenant(gomock.Any()).
		Return(auth.SystemTenant, nil).
		AnyTimes()
	tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
		Return(auth.AllTenants, nil).
		AnyTimes()

	// Create the database server:
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	DeferCleanup(cancel)
	server, err = database.NewContainer().
		SetLogger(logger).
		Build()
	Expect(err).ToNot(HaveOccurred())
	err = server.Start(ctx)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err = server.Stop(ctx)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = BeforeEach(func() {
	var err error

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
	tm, err = database.NewTxManager().
		SetLogger(logger).
		SetPool(pool).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Start a transaction and add it to the context:
	tx, err := tm.Begin(ctx)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		err := tx.End(ctx)
		Expect(err).ToNot(HaveOccurred())
	})
	ctx = database.TxIntoContext(ctx, tx)
})

func dryRunCtx() context.Context {
	md := grpcmetadata.Pairs(DryRunMetadataKey, "true")
	return grpcmetadata.NewIncomingContext(ctx, md)
}

func seedDefaultClusterVersion(ctx context.Context) {
	clusterVersionsDao, err := dao.NewGenericDAO[*privatev1.ClusterVersion]().
		SetLogger(logger).
		SetTenancyLogic(tenancy).
		Build()
	Expect(err).ToNot(HaveOccurred())
	_, err = clusterVersionsDao.Create().
		SetObject(privatev1.ClusterVersion_builder{
			Id: "cv-default",
			Metadata: privatev1.Metadata_builder{
				Name:   "4-17-0",
				Tenant: auth.SharedTenant,
			}.Build(),
			Spec: privatev1.ClusterVersionSpec_builder{
				Image:     "quay.io/openshift-release-dev/ocp-release:4.17.0-multi",
				Enabled:   proto.Bool(true),
				IsDefault: proto.Bool(true),
				Version:   "4.17.0",
				State:     privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE,
			}.Build(),
		}.Build()).
		Do(ctx)
	Expect(err).ToNot(HaveOccurred())
}
