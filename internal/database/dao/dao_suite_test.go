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
	"context"
	"log/slog"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/logging"
)

func TestDAO(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DAO package")
}

var (
	logger *slog.Logger
	server *database.Container
)

// createObjectsTableSQL creates the `objects` and `archived_objects` tables used by tests that exercise the DAO with
// `testsv1.Object`. These tables are not part of any migration because the type only exists for testing purposes.
const createObjectsTableSQL = `
	create table objects (
		id text not null primary key,
		name text not null default '',
		creation_timestamp timestamp with time zone not null default now(),
		deletion_timestamp timestamp with time zone not null default 'epoch',
		finalizers text[] not null default '{}',
		creator text not null default '',
		tenant text not null default '',
		labels jsonb not null default '{}'::jsonb,
		annotations jsonb not null default '{}'::jsonb,
		data jsonb not null default '{}'::jsonb,
		version integer not null default 0
	);
	create table archived_objects (
		id text not null,
		name text not null default '',
		creation_timestamp timestamp with time zone not null,
		deletion_timestamp timestamp with time zone not null,
		archival_timestamp timestamp with time zone not null default now(),
		creator text not null default '',
		tenant text not null default '',
		labels jsonb not null default '{}'::jsonb,
		annotations jsonb not null default '{}'::jsonb,
		version integer not null default 0,
		data jsonb not null
	)
`

var _ = BeforeSuite(func() {
	var err error

	// Create the logger:
	logger, err = logging.NewLogger().
		SetLevel(slog.LevelDebug.String()).
		SetWriter(GinkgoWriter).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Create and start the database server:
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
