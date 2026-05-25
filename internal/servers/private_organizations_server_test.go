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

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Organizations Server", func() {
	var (
		ctx           context.Context
		tx            database.Tx
		privateServer *PrivateOrganizationsServer
	)

	BeforeEach(func() {
		var err error

		// Create context:
		ctx = context.Background()

		// Prepare the database pool:
		db, err := server.NewInstance().Build()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(db.Close)
		pool, err := pgxpool.New(ctx, db.Url())
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

		// Create DAO tables:
		err = dao.CreateTables[*privatev1.Organization](ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create server (without notifier for testing):
		privateServer, err = NewPrivateOrganizationsServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Creates an organization", func() {
		// Create request:
		request := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
				},
			},
		}

		// Create organization:
		response, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object).ToNot(BeNil())
		Expect(response.Object.Id).ToNot(BeEmpty())
		Expect(response.Object.Metadata.Name).To(Equal("test-org"))
	})

	It("Lists organizations", func() {
		// Create an organization first:
		createReq := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
				},
			},
		}
		_, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// List organizations:
		listResp, err := privateServer.List(ctx, &privatev1.OrganizationsListRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Size).To(Equal(int32(1)))
		Expect(listResp.Items).To(HaveLen(1))
		Expect(listResp.Items[0].Metadata.Name).To(Equal("test-org"))
	})

	It("Gets an organization by ID", func() {
		// Create an organization:
		createReq := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Get the organization:
		getResp, err := privateServer.Get(ctx, &privatev1.OrganizationsGetRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
		Expect(getResp.Object.Metadata.Name).To(Equal("test-org"))
	})

	It("Deletes an organization", func() {
		// Create an organization:
		createReq := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Delete the organization:
		_, err = privateServer.Delete(ctx, &privatev1.OrganizationsDeleteRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())
	})

	It("Updates an organization", func() {
		// Create an organization:
		createReq := &privatev1.OrganizationsCreateRequest{
			Object: &privatev1.Organization{
				Metadata: &privatev1.Metadata{
					Name: "test-org",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Update the organization:
		updateReq := &privatev1.OrganizationsUpdateRequest{
			Object: &privatev1.Organization{
				Id: createResp.Object.Id,
				Metadata: &privatev1.Metadata{
					Name: "updated-org",
				},
			},
		}
		updateResp, err := privateServer.Update(ctx, updateReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResp.Object.Metadata.Name).To(Equal("updated-org"))
	})
})
