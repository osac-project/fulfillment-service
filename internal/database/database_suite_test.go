/*
Copyright (c) 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package database

import (
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"github.com/osac-project/fulfillment-common/logging"
	. "github.com/osac-project/fulfillment-common/testing"
)

func TestDatabase(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Database")
}

var (
	logger   *slog.Logger
	dbServer *DatabaseServer
)

var _ = BeforeSuite(func() {
	var err error

	logger, err = logging.NewLogger().
		SetLevel("debug").
		SetOut(GinkgoWriter).
		Build()
	Expect(err).ToNot(HaveOccurred())

	dbServer = MakeDatabaseServer()
	DeferCleanup(dbServer.Close)
})
