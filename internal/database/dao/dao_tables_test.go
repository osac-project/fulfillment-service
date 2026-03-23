/*
Copyright (c) 2025 Red Hat Inc.

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

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	testsv1 "github.com/osac-project/fulfillment-service/internal/api/osac/tests/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
)

var _ = Describe("Create tables", func() {
	var (
		ctx context.Context
		tx  database.Tx
		tm  database.TxManager
	)

	BeforeEach(func() {
		var err error

		// Create a context:
		ctx = context.Background()

		// Prepare the database pool:
		db := server.MakeDatabase()
		DeferCleanup(db.Close)
		pool, err := pgxpool.New(ctx, db.MakeURL())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)

		// Create the transaction manager:
		tm, err = database.NewTxManager().
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
	})

	// tableExists checks if a table exists in the database.
	tableExists := func(tableName string) bool {
		var exists bool
		err := tx.QueryRow(
			ctx,
			`
			select exists (
				select from information_schema.tables
				where table_schema = 'public'
				and table_name = $1
			)
			`,
			tableName,
		).Scan(&exists)
		Expect(err).ToNot(HaveOccurred())
		return exists
	}

	// indexExists checks if an index exists in the database.
	indexExists := func(indexName string) bool {
		var exists bool
		err := tx.QueryRow(
			ctx,
			`
			select exists (
				select from pg_indexes
				where schemaname = 'public'
				and indexname = $1
			)
			`,
			indexName,
		).Scan(&exists)
		Expect(err).ToNot(HaveOccurred())
		return exists
	}

	// columnExists checks if a column exists in a table.
	columnExists := func(tableName, columnName string) bool {
		var exists bool
		err := tx.QueryRow(
			ctx,
			`
			select exists (
				select from information_schema.columns
				where table_schema = 'public'
				and table_name = $1
				and column_name = $2
			)
			`,
			tableName, columnName,
		).Scan(&exists)
		Expect(err).ToNot(HaveOccurred())
		return exists
	}

	Describe("Creating tables", func() {
		It("Creates main table, archived table, and indexes for a single object", func() {
			// Create tables:
			err := CreateTables[*testsv1.Object](ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify main table exists:
			Expect(tableExists("objects")).To(BeTrue())

			// Verify archived table exists:
			Expect(tableExists("archived_objects")).To(BeTrue())

			// Verify indexes exist:
			Expect(indexExists("objects_by_name")).To(BeTrue())
			Expect(indexExists("objects_by_owner")).To(BeTrue())
			Expect(indexExists("objects_by_tenant")).To(BeTrue())
			Expect(indexExists("objects_by_label")).To(BeTrue())
		})

		It("Can be called multiple times without error", func() {
			// Create tables first time:
			err := CreateTables[*testsv1.Object](ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create tables second time (should not fail due to "if not exists"):
			err = CreateTables[*testsv1.Object](ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify tables still exist:
			Expect(tableExists("objects")).To(BeTrue())
			Expect(tableExists("archived_objects")).To(BeTrue())
		})

		It("Creates main table with correct columns", func() {
			// Create tables:
			err := CreateTables[*testsv1.Object](ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify all required columns exist:
			Expect(columnExists("objects", "id")).To(BeTrue())
			Expect(columnExists("objects", "name")).To(BeTrue())
			Expect(columnExists("objects", "creation_timestamp")).To(BeTrue())
			Expect(columnExists("objects", "deletion_timestamp")).To(BeTrue())
			Expect(columnExists("objects", "finalizers")).To(BeTrue())
			Expect(columnExists("objects", "creators")).To(BeTrue())
			Expect(columnExists("objects", "tenants")).To(BeTrue())
			Expect(columnExists("objects", "labels")).To(BeTrue())
			Expect(columnExists("objects", "annotations")).To(BeTrue())
			Expect(columnExists("objects", "data")).To(BeTrue())
		})

		It("Creates archived table with correct columns", func() {
			// Create tables:
			err := CreateTables[*testsv1.Object](ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify all required columns exist:
			Expect(columnExists("archived_objects", "id")).To(BeTrue())
			Expect(columnExists("archived_objects", "name")).To(BeTrue())
			Expect(columnExists("archived_objects", "creation_timestamp")).To(BeTrue())
			Expect(columnExists("archived_objects", "deletion_timestamp")).To(BeTrue())
			Expect(columnExists("archived_objects", "archival_timestamp")).To(BeTrue())
			Expect(columnExists("archived_objects", "creators")).To(BeTrue())
			Expect(columnExists("archived_objects", "tenants")).To(BeTrue())
			Expect(columnExists("archived_objects", "labels")).To(BeTrue())
			Expect(columnExists("archived_objects", "annotations")).To(BeTrue())
			Expect(columnExists("archived_objects", "data")).To(BeTrue())
		})

		It("Handles empty object list", func() {
			// Create tables with no objects:
			err := CreateTables[*testsv1.Object](ctx)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("Error handling", func() {
		It("Returns error when transaction is missing from context", func() {
			// Create context without transaction:
			ctxWithoutTx := context.Background()

			// Try to create tables:
			err := CreateTables[*testsv1.Object](ctxWithoutTx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get transaction from context"))
		})
	})

	Describe("Table structure", func() {
		It("Creates main table with primary key on id column", func() {
			// Create tables:
			err := CreateTables[*testsv1.Object](ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify primary key exists:
			var constraintName string
			err = tx.QueryRow(
				ctx,
				`
				select constraint_name
				from information_schema.table_constraints
				where table_schema = 'public'
				and table_name = 'objects'
				and constraint_type = 'PRIMARY KEY'
				`,
			).Scan(&constraintName)
			Expect(err).ToNot(HaveOccurred())
			Expect(constraintName).ToNot(BeEmpty())
		})

		It("Creates indexes with correct types", func() {
			// Create tables:
			err := CreateTables[*testsv1.Object](ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify index types:
			var indexDef string

			// Check by_name index (should be btree):
			err = tx.QueryRow(
				ctx,
				`
				select indexdef
				from pg_indexes
				where schemaname = 'public'
				and indexname = 'objects_by_name'
				`,
			).Scan(&indexDef)
			Expect(err).ToNot(HaveOccurred())
			Expect(indexDef).To(ContainSubstring("(name)"))

			// Check by_owner index (should be gin):
			err = tx.QueryRow(
				ctx,
				`
				select indexdef
				from pg_indexes
				where schemaname = 'public'
				and indexname = 'objects_by_owner'
				`,
			).Scan(&indexDef)
			Expect(err).ToNot(HaveOccurred())
			Expect(indexDef).To(ContainSubstring("gin"))
			Expect(indexDef).To(ContainSubstring("creators"))

			// Check by_tenant index (should be gin):
			err = tx.QueryRow(
				ctx,
				`
				select indexdef
				from pg_indexes
				where schemaname = 'public'
				and indexname = 'objects_by_tenant'
				`,
			).Scan(&indexDef)
			Expect(err).ToNot(HaveOccurred())
			Expect(indexDef).To(ContainSubstring("gin"))
			Expect(indexDef).To(ContainSubstring("tenants"))

			// Check by_label index (should be gin):
			err = tx.QueryRow(
				ctx,
				`
				select indexdef
				from pg_indexes
				where schemaname = 'public'
				and indexname = 'objects_by_label'
				`,
			).Scan(&indexDef)
			Expect(err).ToNot(HaveOccurred())
			Expect(indexDef).To(ContainSubstring("gin"))
			Expect(indexDef).To(ContainSubstring("labels"))
		})
	})
})
