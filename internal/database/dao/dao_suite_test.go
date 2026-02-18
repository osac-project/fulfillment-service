/*
Copyright (c) 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package dao

import (
	"log/slog"
	"testing"

	"go.uber.org/mock/gomock"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"github.com/osac-project/fulfillment-common/logging"
	. "github.com/osac-project/fulfillment-common/testing"

	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
)

func TestDAO(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DAO")
}

var (
	logger      *slog.Logger
	server      *DatabaseServer
	ctrl        *gomock.Controller
	attribution *auth.MockAttributionLogic
	tenancy     *auth.MockTenancyLogic
)

var _ = BeforeSuite(func() {
	var err error

	// Create the logger:
	logger, err = logging.NewLogger().
		SetLevel(slog.LevelDebug.String()).
		SetWriter(GinkgoWriter).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Create the mock controller:
	ctrl = gomock.NewController(GinkgoT())
	DeferCleanup(ctrl.Finish)

	// Create the attribution logic:
	creators := collections.NewSet("my-user")
	attribution = auth.NewMockAttributionLogic(ctrl)
	attribution.EXPECT().DetermineAssignedCreators(gomock.Any()).
		Return(creators, nil).
		AnyTimes()

	// Create the tenancy logic:
	tenants := collections.NewSet("my-tenant")
	tenancy = auth.NewMockTenancyLogic(ctrl)
	tenancy.EXPECT().DetermineAssignableTenants(gomock.Any()).
		Return(tenants, nil).
		AnyTimes()
	tenancy.EXPECT().DetermineDefaultTenants(gomock.Any()).
		Return(tenants, nil).
		AnyTimes()
	tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
		Return(tenants, nil).
		AnyTimes()

	// Create the database server:
	server = MakeDatabaseServer()
	DeferCleanup(server.Close)
})
