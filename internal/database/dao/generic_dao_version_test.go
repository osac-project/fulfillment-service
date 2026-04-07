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
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	testsv1 "github.com/osac-project/fulfillment-service/internal/api/osac/tests/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
	"github.com/osac-project/fulfillment-service/internal/database"
)

var _ = Describe("Version", func() {
	var (
		ctx     context.Context
		tx      database.Tx
		generic *GenericDAO[*testsv1.Object]
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
		err = CreateTables[*testsv1.Object](ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create the attribution logic:
		attributionLogic := auth.NewMockAttributionLogic(ctrl)
		attributionLogic.EXPECT().DetermineAssignedCreators(gomock.Any()).
			Return(
				collections.NewSet("my_user"),
				nil,
			).
			AnyTimes()

		// Create the tenancy logic:
		tenancyLogic := auth.NewMockTenancyLogic(ctrl)
		tenancyLogic.EXPECT().DetermineAssignableTenants(gomock.Any()).
			Return(
				collections.NewSet("my_tenant"),
				nil,
			).
			AnyTimes()
		tenancyLogic.EXPECT().DetermineDefaultTenants(gomock.Any()).
			Return(
				collections.NewSet("my_tenant"),
				nil,
			).
			AnyTimes()
		tenancyLogic.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(
				collections.NewSet("my_tenant"),
				nil,
			).
			AnyTimes()

		// Create the DAO:
		generic, err = NewGenericDAO[*testsv1.Object]().
			SetLogger(logger).
			SetAttributionLogic(attributionLogic).
			SetTenancyLogic(tenancyLogic).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	// checkDatabaseVersion checks the version of the object in the database.
	checkDatabaseVersion := func(id string, expected int32) {
		row := tx.QueryRow(ctx, "select version from objects where id = $1", id)
		var actual int32
		err := row.Scan(&actual)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(expected))
	}

	It("Is zero on create", func() {
		response, err := generic.Create().
			SetObject(
				testsv1.Object_builder{
					MyString: "my_value",
				}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object := response.GetObject()
		Expect(object.GetMetadata().GetVersion()).To(Equal(int32(0)))
		checkDatabaseVersion(object.GetId(), 0)
	})

	It("Is zero when retrieved after create", func() {
		createResponse, err := generic.Create().
			SetObject(
				testsv1.Object_builder{
					MyString: "my_value",
				}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()

		getResponse, err := generic.Get().
			SetId(object.GetId()).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object = getResponse.GetObject()
		Expect(object.GetMetadata().GetVersion()).To(Equal(int32(0)))
	})

	It("Is zero when listed after create", func() {
		createResponse, err := generic.Create().
			SetObject(
				testsv1.Object_builder{
					MyString: "my_value",
				}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()

		listResponse, err := generic.List().
			SetFilter(fmt.Sprintf("this.id == '%s'", id)).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		items := listResponse.GetItems()
		Expect(items).To(HaveLen(1))
		Expect(items[0].GetMetadata().GetVersion()).To(Equal(int32(0)))
	})

	It("Increments on update", func() {
		createResponse, err := generic.Create().
			SetObject(
				testsv1.Object_builder{
					MyString: "my_value",
				}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()
		Expect(object.GetMetadata().GetVersion()).To(Equal(int32(0)))

		// First update:
		object.MyString = "your_value"
		updateResponse, err := generic.Update().
			SetObject(object).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object = updateResponse.GetObject()
		Expect(object.GetMetadata().GetVersion()).To(Equal(int32(1)))
		checkDatabaseVersion(object.GetId(), 1)

		// Second update:
		object.MyString = "another_value"
		updateResponse, err = generic.Update().
			SetObject(object).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object = updateResponse.GetObject()
		Expect(object.GetMetadata().GetVersion()).To(Equal(int32(2)))
		checkDatabaseVersion(object.GetId(), 2)
	})

	It("Does not increment on no-op update", func() {
		createResponse, err := generic.Create().
			SetObject(
				testsv1.Object_builder{
					MyString: "my_value",
				}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()
		Expect(object.GetMetadata().GetVersion()).To(Equal(int32(0)))

		// Update with the same data:
		updateResponse, err := generic.Update().
			SetObject(object).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object = updateResponse.GetObject()
		Expect(object.GetMetadata().GetVersion()).To(Equal(int32(0)))
		checkDatabaseVersion(object.GetId(), 0)
	})

	It("Increments on each distinct update", func() {
		createResponse, err := generic.Create().
			SetObject(
				testsv1.Object_builder{
					MyInt32: 0,
				}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()

		for i := int32(1); i <= 5; i++ {
			object.MyInt32 = i
			updateResponse, err := generic.Update().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			Expect(object.GetMetadata().GetVersion()).To(Equal(i))
		}
		checkDatabaseVersion(object.GetId(), 5)
	})

	It("Matches database after get", func() {
		createResponse, err := generic.Create().
			SetObject(
				testsv1.Object_builder{
					MyString: "my_value",
				}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()

		// Perform a couple of updates:
		object.MyString = "value_1"
		updateResponse, err := generic.Update().
			SetObject(object).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object = updateResponse.GetObject()

		object.MyString = "value_2"
		updateResponse, err = generic.Update().
			SetObject(object).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object = updateResponse.GetObject()

		// Get and verify the version matches:
		getResponse, err := generic.Get().
			SetId(object.GetId()).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		retrieved := getResponse.GetObject()
		Expect(retrieved.GetMetadata().GetVersion()).To(Equal(int32(2)))
		checkDatabaseVersion(retrieved.GetId(), 2)
	})

	It("Ignores user-provided version and increments correctly", func() {
		// Create an object (version 0):
		createResponse, err := generic.Create().
			SetObject(
				testsv1.Object_builder{
					MyString: "v0",
				}.Build(),
			).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object := createResponse.GetObject()
		Expect(object.GetMetadata().GetVersion()).To(Equal(int32(0)))

		// Perform three updates to reach version 3:
		for i := int32(1); i <= 3; i++ {
			object.MyString = fmt.Sprintf("v%d", i)
			updateResponse, err := generic.Update().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
		}
		Expect(object.GetMetadata().GetVersion()).To(Equal(int32(3)))

		// Now try to set the version to 2 while making a real change. The user-provided
		// version should be ignored and the database should increment to 4:
		object.GetMetadata().SetVersion(2)
		object.MyString = "v4"
		updateResponse, err := generic.Update().
			SetObject(object).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		object = updateResponse.GetObject()
		Expect(object.GetMetadata().GetVersion()).To(Equal(int32(4)))
		checkDatabaseVersion(object.GetId(), 4)
	})

	Describe("Optimistic locking", func() {
		It("Succeeds when version matches", func() {
			// Create the object:
			createResponse, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						MyString: "my-value",
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			version := object.GetMetadata().GetVersion()

			// Update without locking a few times:
			for i := 1; i <= 2; i++ {
				object.MyString = fmt.Sprintf("your-value-%d", i)
				updateResponse, err := generic.Update().
					SetObject(object).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object = updateResponse.GetObject()
				Expect(object.GetMetadata().GetVersion()).To(BeNumerically(">", version))
				version = object.GetMetadata().GetVersion()
			}

			// Update with lock enabled and the right version:
			object.MyString = "their-value"
			updateResponse, err := generic.Update().
				SetObject(object).
				SetLock(true).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			Expect(object.GetMetadata().GetVersion()).To(BeNumerically(">", version))
		})

		It("Fails when version does not match", func() {
			// Create the object:
			createResponse, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						MyString: "my-value",
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			version := object.GetMetadata().GetVersion()

			// Set a stale version in the metadata and try to update with lock enabled:
			object.GetMetadata().SetVersion(math.MaxInt32)
			object.MyString = "your-value"
			_, err = generic.Update().
				SetObject(object).
				SetLock(true).
				Do(ctx)
			Expect(err).To(HaveOccurred())
			var conflictErr *ErrConflict
			Expect(errors.As(err, &conflictErr)).To(BeTrue())
			Expect(conflictErr.ID).To(Equal(object.GetId()))
			Expect(conflictErr.RequestedVersion).To(BeNumerically("==", math.MaxInt32))
			Expect(conflictErr.CurrentVersion).To(BeNumerically("==", version))
		})

		It("Is dsabled by default", func() {
			// Create the object:
			createResponse, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						MyString: "my-value",
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Send an update with a wrong version in the metadata but without enabling lock. The update
			// should succeed because optimistic locking is disabled.
			object.GetMetadata().SetVersion(math.MaxInt32)
			object.MyString = "your-value"
			_, err = generic.Update().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
