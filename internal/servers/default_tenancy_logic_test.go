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
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
	"github.com/osac-project/fulfillment-service/internal/database"
)

var _ = Describe("Default tenancy logic", func() {
	var logic *DefaultTenancyLogic

	BeforeEach(func() {
		var err error
		logic, err = NewDefaultTenancyLogic().
			SetLogger(logger).
			Build()
		Expect(err).ToNot(HaveOccurred())
		Expect(logic).ToNot(BeNil())
	})

	Describe("Builder", func() {
		It("Fails if logger is not set", func() {
			logic, err := NewDefaultTenancyLogic().
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger is mandatory"))
			Expect(logic).To(BeNil())
		})
	})

	Describe("Determine assignable tenants", func() {
		It("Returns the tenants from the subject", func() {
			subject := &auth.Subject{
				User:    "my_user",
				Tenants: collections.NewSet("tenant-a", "tenant-b"),
			}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineAssignableTenants(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("tenant-a", "tenant-b"))).To(BeTrue())
		})

		It("Returns multiple tenants when present", func() {
			subject := &auth.Subject{
				User:    "my_user",
				Tenants: collections.NewSet("tenant-a", "tenant-b", "tenant-c"),
			}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineAssignableTenants(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("tenant-a", "tenant-b", "tenant-c"))).To(BeTrue())
		})

		It("Returns universal set when tenants is universal", func() {
			subject := &auth.Subject{
				User:    "my_user",
				Tenants: auth.AllTenants,
			}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineAssignableTenants(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(auth.AllTenants)).To(BeTrue())
		})

		It("Fails if the subject has an empty tenants set", func() {
			subject := &auth.Subject{
				User:    "my_user",
				Tenants: collections.NewSet[string](),
			}
			myCtx := auth.ContextWithSubject(ctx, subject)
			_, err := logic.DetermineAssignableTenants(myCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("at least one tenant"))
		})

		It("Returns an empty set when there is an error", func() {
			subject := &auth.Subject{
				User: "my_user",
			}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineAssignableTenants(myCtx)
			Expect(err).To(HaveOccurred())
			Expect(result.Empty()).To(BeTrue())
		})
	})

	Describe("Determine default tenant", func() {
		It("Returns a tenant from the subject", func() {
			subject := &auth.Subject{
				User:    "my_user",
				Tenants: collections.NewSet("tenant-a", "tenant-b"),
			}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineDefaultTenant(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeElementOf("tenant-a", "tenant-b"))
		})

		It("Returns shared when tenants is universal", func() {
			subject := &auth.Subject{
				User:    "my_user",
				Tenants: auth.AllTenants,
			}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineDefaultTenant(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(auth.SharedTenant))
		})

		It("Fails if the subject has an empty tenants set", func() {
			subject := &auth.Subject{
				User:    "my_user",
				Tenants: collections.NewSet[string](),
			}
			myCtx := auth.ContextWithSubject(ctx, subject)
			_, err := logic.DetermineDefaultTenant(myCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("at least one tenant"))
		})
	})

	Describe("Visibility", func() {
		// createTenant inserts a tenant row. The database trigger automatically creates the default project for
		// the tenant.
		createTenant := func(tx database.Tx, name string) {
			_, err := tx.Exec(
				ctx,
				`
				insert into tenants (
					id,
					tenant,
					name,
					data
				)
				values (
					$1,
					$2,
					$3,
					'{}'
				)`,
				name, name, name,
			)
			Expect(err).ToNot(HaveOccurred())
		}

		// createMembershp inserts a project_membership row. The database trigger automatically populates the
		// project_membership_subjects helper table.
		createMembershp := func(tx database.Tx, id, tenantName, project, user string) {
			dataMap := map[string]any{
				"spec": map[string]any{
					"project": project,
					"user":    user,
					"role":    "PROJECT_MEMBERSHIP_ROLE_VIEWER",
				},
			}
			dataJson, err := json.Marshal(dataMap)
			Expect(err).ToNot(HaveOccurred())
			_, err = tx.Exec(
				ctx,
				`
				insert into project_memberships (
					id,
					tenant,
					data
				)
				values (
					$1,
					$2,
					$3
				)
					`,
				id, tenantName, dataJson,
			)
			ExpectWithOffset(1, err).ToNot(HaveOccurred())
		}

		It("Includes the shared tenant with the default project", func() {
			subject := &auth.Subject{User: "my_user"}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibility(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.HasTenant(auth.SharedTenant)).To(BeTrue())
			Expect(result.HasProject(auth.SharedTenant, auth.DefaultProject)).To(BeTrue())
		})

		It("Includes project memberships from the database", func() {
			tx, err := database.TxFromContext(ctx)
			Expect(err).ToNot(HaveOccurred())
			createTenant(tx, "tenant-a")
			createTenant(tx, "tenant-b")
			createMembershp(tx, "pm-1", "tenant-a", "project-x", "my_user")
			createMembershp(tx, "pm-2", "tenant-b", "project-y", "my_user")

			subject := &auth.Subject{User: "my_user"}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibility(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.HasProject("tenant-a", "project-x")).To(BeTrue())
			Expect(result.HasProject("tenant-b", "project-y")).To(BeTrue())
		})

		It("Does not grant access to projects that are not in the database", func() {
			tx, err := database.TxFromContext(ctx)
			Expect(err).ToNot(HaveOccurred())
			createTenant(tx, "tenant-a")
			createMembershp(tx, "pm-1", "tenant-a", "project-x", "my_user")

			subject := &auth.Subject{User: "my_user"}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibility(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.HasProject("tenant-a", "project-y")).To(BeFalse())
			Expect(result.HasProject("tenant-b", "project-x")).To(BeFalse())
		})

		It("Does not include memberships belonging to other users", func() {
			tx, err := database.TxFromContext(ctx)
			Expect(err).ToNot(HaveOccurred())
			createTenant(tx, "tenant-a")
			createMembershp(tx, "pm-1", "tenant-a", "project-x", "other_user")

			subject := &auth.Subject{User: "my_user"}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibility(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.HasProject("tenant-a", "project-x")).To(BeFalse())
		})

		It("Fails when there is no transaction in the context", func() {
			subject := &auth.Subject{User: "my_user"}
			myCtx := auth.ContextWithSubject(context.Background(), subject)
			_, err := logic.DetermineVisibility(myCtx)
			Expect(err).To(HaveOccurred())
		})

		It("Returns total visibility when subject has universal tenants", func() {
			subject := &auth.Subject{
				User:    "my_admin",
				Tenants: auth.AllTenants,
			}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibility(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Total()).To(BeTrue())
		})

		It("Total visibility grants access to any tenant", func() {
			subject := &auth.Subject{
				User:    "my_admin",
				Tenants: auth.AllTenants,
			}
			myCtx := auth.ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibility(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.HasTenant("any-tenant")).To(BeTrue())
			Expect(result.HasProject("any-tenant", "any-project")).To(BeTrue())
		})

		It("Total visibility does not require a database transaction", func() {
			subject := &auth.Subject{
				User:    "my_admin",
				Tenants: auth.AllTenants,
			}
			myCtx := auth.ContextWithSubject(context.Background(), subject)
			result, err := logic.DetermineVisibility(myCtx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Total()).To(BeTrue())
		})
	})
})
