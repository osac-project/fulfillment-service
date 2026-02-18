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
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"github.com/osac-project/fulfillment-common/logging"
	. "github.com/osac-project/fulfillment-common/testing"

	"github.com/osac-project/fulfillment-service/internal/auth"
)

func TestServers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Servers")
}

var (
	logger      *slog.Logger
	server      *DatabaseServer
	attribution auth.AttributionLogic
	tenancy     auth.TenancyLogic
)

var _ = BeforeSuite(func() {
	var err error

	// Create the logger:
	logger, err = logging.NewLogger().
		SetLevel(slog.LevelDebug.String()).
		SetWriter(GinkgoWriter).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Create the attribution logic:
	attribution, err = auth.NewSystemAttributionLogic().
		SetLogger(logger).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Create the tenancy logic:
	tenancy, err = auth.NewSystemTenancyLogic().
		SetLogger(logger).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Create the database server:
	server = MakeDatabaseServer()
	DeferCleanup(server.Close)
})
