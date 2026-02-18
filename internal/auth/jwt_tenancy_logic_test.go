/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package auth

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/osac-project/fulfillment-service/internal/collections"
)

var _ = Describe("JWT tenancy logic", func() {
	var (
		ctx   context.Context
		logic *JwtTenancyLogic
	)

	BeforeEach(func() {
		var err error

		// Create the context:
		ctx = context.Background()

		// Create the tenancy logic:
		logic, err = NewJwtTenancyLogic().
			SetLogger(logger).
			Build()
		Expect(err).ToNot(HaveOccurred())
		Expect(logic).ToNot(BeNil())
	})

	Describe("Builder", func() {
		It("Fails if logger is not set", func() {
			logic, err := NewJwtTenancyLogic().
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(logic).To(BeNil())
		})
	})

	Describe("Determine assignable tenants", func() {
		It("Returns the groups as assignable tenants", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "my_user",
				Groups: []string{"group1", "group2"},
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineAssignableTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("group1", "group2"))).To(BeTrue())
		})

		It("Returns error when user has no groups", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "my_user",
				Groups: []string{},
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineAssignableTenants(ctx)
			Expect(err).To(MatchError("user must belong to at least one group to create objects"))
			Expect(result.Empty()).To(BeTrue())
		})
	})

	Describe("Determine default tenants", func() {
		It("Returns the groups as default tenants", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "my_user",
				Groups: []string{"group1", "group2"},
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineDefaultTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("group1", "group2"))).To(BeTrue())
		})

		It("Returns error when user has no groups", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "my_user",
				Groups: []string{},
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineDefaultTenants(ctx)
			Expect(err).To(MatchError("user must belong to at least one group to create objects"))
			Expect(result.Empty()).To(BeTrue())
		})
	})

	Describe("Determine visible tenants", func() {
		It("Returns only shared when user has no groups", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "my_user",
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibleTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("shared"))).To(BeTrue())
		})

		It("Returns the groups and shared", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "my_user",
				Groups: []string{"group1", "group2"},
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibleTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("group1", "group2", "shared"))).To(BeTrue())
		})

		It("Returns multiple groups and shared", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "admin@example.com",
				Groups: []string{"admins", "developers", "team-a"},
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibleTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("admins", "developers", "team-a", "shared"))).To(BeTrue())
		})

		It("Returns only shared when groups is empty array", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "user_with_empty_groups",
				Groups: []string{},
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibleTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("shared"))).To(BeTrue())
		})
	})
})
