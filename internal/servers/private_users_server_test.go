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

var _ = Describe("Users Server", func() {
	var (
		ctx           context.Context
		tx            database.Tx
		privateServer *PrivateUsersServer
	)

	BeforeEach(func() {
		var err error

		// Create context:
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

		// Create DAO tables:
		err = dao.CreateTables[*privatev1.User](ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create server (without notifier for testing):
		privateServer, err = NewPrivateUsersServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Creates a user", func() {
		// Create request:
		request := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
					Name: "test-user",
				},
				Spec: &privatev1.UserSpec{
					Username:      "testuser",
					Email:         "test@example.com",
					EmailVerified: true,
					Enabled:       true,
					FirstName:     "Test",
					LastName:      "User",
					Organization:  "org-123",
				},
			},
		}

		// Create user:
		response, err := privateServer.Create(ctx, request)
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object).ToNot(BeNil())
		Expect(response.Object.Id).ToNot(BeEmpty())
		Expect(response.Object.Metadata.Name).To(Equal("test-user"))
		Expect(response.Object.Spec.Username).To(Equal("testuser"))
	})

	It("Lists users", func() {
		// Create a user first:
		createReq := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
					Name: "test-user",
				},
				Spec: &privatev1.UserSpec{
					Username: "testuser",
					Email:    "test@example.com",
				},
			},
		}
		_, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// List users:
		listResp, err := privateServer.List(ctx, &privatev1.UsersListRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.Size).To(Equal(int32(1)))
		Expect(listResp.Items).To(HaveLen(1))
		Expect(listResp.Items[0].Metadata.Name).To(Equal("test-user"))
	})

	It("Gets a user by ID", func() {
		// Create a user:
		createReq := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
					Name: "test-user",
				},
				Spec: &privatev1.UserSpec{
					Username: "testuser",
					Email:    "test@example.com",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Get the user:
		getResp, err := privateServer.Get(ctx, &privatev1.UsersGetRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.Object.Id).To(Equal(createResp.Object.Id))
		Expect(getResp.Object.Metadata.Name).To(Equal("test-user"))
	})

	It("Deletes a user", func() {
		// Create a user:
		createReq := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
					Name: "test-user",
				},
				Spec: &privatev1.UserSpec{
					Username: "testuser",
					Email:    "test@example.com",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Delete the user:
		_, err = privateServer.Delete(ctx, &privatev1.UsersDeleteRequest{
			Id: createResp.Object.Id,
		})
		Expect(err).ToNot(HaveOccurred())
	})

	It("Updates a user", func() {
		// Create a user:
		createReq := &privatev1.UsersCreateRequest{
			Object: &privatev1.User{
				Metadata: &privatev1.Metadata{
					Name: "test-user",
				},
				Spec: &privatev1.UserSpec{
					Username:  "testuser",
					Email:     "test@example.com",
					FirstName: "Original",
				},
			},
		}
		createResp, err := privateServer.Create(ctx, createReq)
		Expect(err).ToNot(HaveOccurred())

		// Update the user:
		updateReq := &privatev1.UsersUpdateRequest{
			Object: &privatev1.User{
				Id: createResp.Object.Id,
				Spec: &privatev1.UserSpec{
					FirstName: "Updated",
				},
			},
		}
		updateResp, err := privateServer.Update(ctx, updateReq)
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResp.Object.Spec.FirstName).To(Equal("Updated"))
	})
})
