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
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Public role bindings server", func() {
	var (
		ctx                context.Context
		tx                 database.Tx
		roleBindingsServer *RoleBindingsServer
	)

	BeforeEach(func() {
		var err error

		ctx = context.Background()

		db, err := server.NewInstance().Build()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(db.Close)
		pool, err := pgxpool.New(ctx, db.Url())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)

		tm, err := database.NewTxManager().
			SetLogger(logger).
			SetPool(pool).
			Build()
		Expect(err).ToNot(HaveOccurred())

		tx, err = tm.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := tm.End(ctx, tx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)

		err = dao.CreateTables[*privatev1.RoleBinding](ctx)
		Expect(err).ToNot(HaveOccurred())

		roleBindingsServer, err = NewRoleBindingsServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all required parameters are set", func() {
			server, err := NewRoleBindingsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewRoleBindingsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewRoleBindingsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		It("Creates a role binding", func() {
			request := publicv1.RoleBindingsCreateRequest_builder{
				Object: publicv1.RoleBinding_builder{
					Metadata: publicv1.Metadata_builder{
						Name: "test-binding",
					}.Build(),
					Spec: publicv1.RoleBindingSpec_builder{
						Role: "my-role-id",
						Groups: []string{
							"group-a",
							"group-b",
						},
					}.Build(),
				}.Build(),
			}.Build()

			response, err := roleBindingsServer.Create(ctx, request)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.GetObject()).ToNot(BeNil())
			Expect(response.GetObject().GetId()).ToNot(BeEmpty())
			Expect(response.GetObject().GetMetadata().GetName()).To(Equal("test-binding"))
			Expect(response.GetObject().GetSpec().GetRole()).To(Equal("my-role-id"))
			Expect(response.GetObject().GetSpec().GetGroups()).To(ConsistOf("group-a", "group-b"))
		})

		It("Lists role bindings", func() {
			_, err := roleBindingsServer.Create(ctx, publicv1.RoleBindingsCreateRequest_builder{
				Object: publicv1.RoleBinding_builder{
					Metadata: publicv1.Metadata_builder{
						Name: "binding-1",
					}.Build(),
					Spec: publicv1.RoleBindingSpec_builder{
						Role: "role-1",
						Groups: []string{
							"group-x",
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			listResponse, err := roleBindingsServer.List(ctx, publicv1.RoleBindingsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(listResponse.GetSize()).To(Equal(int32(1)))
			Expect(listResponse.GetItems()).To(HaveLen(1))
			Expect(listResponse.GetItems()[0].GetSpec().GetRole()).To(Equal("role-1"))
		})

		It("Gets a role binding", func() {
			createResponse, err := roleBindingsServer.Create(ctx, publicv1.RoleBindingsCreateRequest_builder{
				Object: publicv1.RoleBinding_builder{
					Metadata: publicv1.Metadata_builder{
						Name: "test-binding",
					}.Build(),
					Spec: publicv1.RoleBindingSpec_builder{
						Role: "role-1",
						Groups: []string{
							"group-a",
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := roleBindingsServer.Get(ctx, publicv1.RoleBindingsGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetId()).To(Equal(createResponse.GetObject().GetId()))
			Expect(getResponse.GetObject().GetSpec().GetRole()).To(Equal("role-1"))
			Expect(getResponse.GetObject().GetSpec().GetGroups()).To(ConsistOf("group-a"))
		})

		It("Updates a role binding", func() {
			createResponse, err := roleBindingsServer.Create(ctx, publicv1.RoleBindingsCreateRequest_builder{
				Object: publicv1.RoleBinding_builder{
					Metadata: publicv1.Metadata_builder{
						Name: "test-binding",
					}.Build(),
					Spec: publicv1.RoleBindingSpec_builder{
						Role: "role-1",
						Groups: []string{
							"group-a",
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			updateResponse, err := roleBindingsServer.Update(ctx, publicv1.RoleBindingsUpdateRequest_builder{
				Object: publicv1.RoleBinding_builder{
					Id: createResponse.GetObject().GetId(),
					Spec: publicv1.RoleBindingSpec_builder{
						Groups: []string{
							"group-a",
							"group-b",
						},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"spec.groups",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetSpec().GetGroups()).To(ConsistOf("group-a", "group-b"))
		})

		It("Ignores fields not included in the update mask", func() {
			createResponse, err := roleBindingsServer.Create(ctx, publicv1.RoleBindingsCreateRequest_builder{
				Object: publicv1.RoleBinding_builder{
					Metadata: publicv1.Metadata_builder{
						Name: "test-binding",
					}.Build(),
					Spec: publicv1.RoleBindingSpec_builder{
						Role: "role-1",
						Groups: []string{
							"group-a",
							"group-b",
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			updateResponse, err := roleBindingsServer.Update(ctx, publicv1.RoleBindingsUpdateRequest_builder{
				Object: publicv1.RoleBinding_builder{
					Id: createResponse.GetObject().GetId(),
					Spec: publicv1.RoleBindingSpec_builder{
						Role: "role-2",
						Groups: []string{
							"group-x",
						},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"spec.role",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetSpec().GetRole()).To(Equal("role-2"))
			Expect(updateResponse.GetObject().GetSpec().GetGroups()).To(ConsistOf("group-a", "group-b"))
		})

		It("Deletes a role binding", func() {
			createResponse, err := roleBindingsServer.Create(ctx, publicv1.RoleBindingsCreateRequest_builder{
				Object: publicv1.RoleBinding_builder{
					Metadata: publicv1.Metadata_builder{
						Name: "test-binding",
					}.Build(),
					Spec: publicv1.RoleBindingSpec_builder{
						Role: "role-1",
						Groups: []string{
							"group-a",
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = roleBindingsServer.Delete(ctx, publicv1.RoleBindingsDeleteRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Returns error when creating with nil object", func() {
			_, err := roleBindingsServer.Create(ctx, publicv1.RoleBindingsCreateRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
		})

		It("Returns error when updating with nil object", func() {
			_, err := roleBindingsServer.Update(ctx, publicv1.RoleBindingsUpdateRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
		})

		It("Returns error when updating without object identifier", func() {
			_, err := roleBindingsServer.Update(ctx, publicv1.RoleBindingsUpdateRequest_builder{
				Object: publicv1.RoleBinding_builder{
					Spec: publicv1.RoleBindingSpec_builder{
						Role: "role-1",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
		})
	})
})
