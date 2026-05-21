/*
Copyright (c) 2026 Red Hat Inc.

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
	"go.uber.org/mock/gomock"

	testsv1 "github.com/osac-project/fulfillment-service/internal/api/osac/tests/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
	"github.com/osac-project/fulfillment-service/internal/database"
)

var _ = Describe("Project visibility", func() {
	var (
		ctx  context.Context
		tx   database.Tx
		ctrl *gomock.Controller
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

		// Create the objects table:
		err = CreateTables[*testsv1.Object](ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create the mock controller:
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)
	})

	// newDAO creates a DAO with universal tenant visibility and the given project visibility.
	newDAO := func(projects collections.Set[string]) *GenericDAO[*testsv1.Object] {
		tenancy := auth.NewMockTenancyLogic(ctrl)
		tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(collections.NewUniversalSet[string](), nil).
			AnyTimes()
		tenancy.EXPECT().DetermineVisibleProjects(gomock.Any()).
			Return(projects, nil).
			AnyTimes()
		dao, err := NewGenericDAO[*testsv1.Object]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		return dao
	}

	// createObject creates an object with the given tenant and project.
	createObject := func(dao *GenericDAO[*testsv1.Object], tenant, project string) *testsv1.Object {
		response, err := dao.Create().
			SetObject(testsv1.Object_builder{
				Metadata: testsv1.Metadata_builder{
					Tenant:  tenant,
					Project: project,
				}.Build(),
			}.Build()).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		return response.GetObject()
	}

	It("Lists only objects belonging to visible projects within the same tenant", func() {
		adminDAO := newDAO(collections.NewUniversalSet[string]())
		createObject(adminDAO, "tenant_a", "project_x")
		createObject(adminDAO, "tenant_a", "project_y")
		createObject(adminDAO, "tenant_a", "project_z")

		userDAO := newDAO(collections.NewSet("project_x", "project_z"))
		listResponse, err := userDAO.List().Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(listResponse.GetItems()).To(HaveLen(2))
		projects := make([]string, len(listResponse.GetItems()))
		for i, item := range listResponse.GetItems() {
			projects[i] = item.GetMetadata().GetProject()
		}
		Expect(projects).To(ConsistOf("project_x", "project_z"))
	})

	It("Returns object via Get when project is visible", func() {
		adminDAO := newDAO(collections.NewUniversalSet[string]())
		object := createObject(adminDAO, "tenant_a", "project_x")

		userDAO := newDAO(collections.NewSet("project_x"))
		getResponse, err := userDAO.Get().
			SetId(object.GetId()).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(getResponse.GetObject().GetId()).To(Equal(object.GetId()))
		Expect(getResponse.GetObject().GetMetadata().GetProject()).To(Equal("project_x"))
	})

	It("Rejects Get when project is not visible", func() {
		adminDAO := newDAO(collections.NewUniversalSet[string]())
		object := createObject(adminDAO, "tenant_a", "project_x")

		userDAO := newDAO(collections.NewSet("project_y"))
		_, err := userDAO.Get().
			SetId(object.GetId()).
			Do(ctx)
		var notFoundErr *ErrNotFound
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeAssignableToTypeOf(notFoundErr))
	})

	It("Shows all projects when user has universal project visibility", func() {
		adminDAO := newDAO(collections.NewUniversalSet[string]())
		createObject(adminDAO, "tenant_a", "project_x")
		createObject(adminDAO, "tenant_a", "project_y")

		listResponse, err := adminDAO.List().Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(listResponse.GetItems()).To(HaveLen(2))
	})

	It("Returns empty list when user has no visible projects", func() {
		adminDAO := newDAO(collections.NewUniversalSet[string]())
		createObject(adminDAO, "tenant_a", "project_x")

		emptyDAO := newDAO(collections.NewSet[string]())
		listResponse, err := emptyDAO.List().Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(listResponse.GetItems()).To(BeEmpty())
	})

	It("Allows update only when project is visible", func() {
		adminDAO := newDAO(collections.NewUniversalSet[string]())
		object := createObject(adminDAO, "tenant_a", "project_x")

		userDAO := newDAO(collections.NewSet("project_x"))
		object.SetMyString("updated")
		updateResponse, err := userDAO.Update().
			SetObject(object).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResponse.GetObject().GetMyString()).To(Equal("updated"))
	})

	It("Rejects update when project is not visible", func() {
		adminDAO := newDAO(collections.NewUniversalSet[string]())
		object := createObject(adminDAO, "tenant_a", "project_x")

		userDAO := newDAO(collections.NewSet("project_y"))
		object.SetMyString("updated")
		_, err := userDAO.Update().
			SetObject(object).
			Do(ctx)
		var notFoundErr *ErrNotFound
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeAssignableToTypeOf(notFoundErr))
	})

	It("Allows delete only when project is visible", func() {
		adminDAO := newDAO(collections.NewUniversalSet[string]())
		object := createObject(adminDAO, "tenant_a", "project_x")

		userDAO := newDAO(collections.NewSet("project_x"))
		_, err := userDAO.Delete().
			SetId(object.GetId()).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())

		_, err = userDAO.Get().
			SetId(object.GetId()).
			Do(ctx)
		var notFoundErr *ErrNotFound
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeAssignableToTypeOf(notFoundErr))
	})

	It("Rejects delete when project is not visible", func() {
		adminDAO := newDAO(collections.NewUniversalSet[string]())
		object := createObject(adminDAO, "tenant_a", "project_x")

		userDAO := newDAO(collections.NewSet("project_y"))
		_, err := userDAO.Delete().
			SetId(object.GetId()).
			Do(ctx)
		var notFoundErr *ErrNotFound
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeAssignableToTypeOf(notFoundErr))

		// Verify the object still exists:
		getResponse, err := adminDAO.Get().
			SetId(object.GetId()).
			Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(getResponse.GetObject().GetId()).To(Equal(object.GetId()))
	})

	It("Filters by both tenant and project simultaneously", func() {
		adminDAO := newDAO(collections.NewUniversalSet[string]())
		createObject(adminDAO, "tenant_a", "project_x")
		createObject(adminDAO, "tenant_a", "project_y")
		createObject(adminDAO, "tenant_b", "project_x")
		createObject(adminDAO, "tenant_b", "project_y")

		// Create a DAO that can only see tenant_a and project_x:
		tenancy := auth.NewMockTenancyLogic(ctrl)
		tenancy.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(collections.NewSet("tenant_a"), nil).
			AnyTimes()
		tenancy.EXPECT().DetermineVisibleProjects(gomock.Any()).
			Return(collections.NewSet("project_x"), nil).
			AnyTimes()
		restrictedDAO, err := NewGenericDAO[*testsv1.Object]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		listResponse, err := restrictedDAO.List().Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(listResponse.GetItems()).To(HaveLen(1))
		item := listResponse.GetItems()[0]
		Expect(item.GetMetadata().GetTenant()).To(Equal("tenant_a"))
		Expect(item.GetMetadata().GetProject()).To(Equal("project_x"))
	})
})
