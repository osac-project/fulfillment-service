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

var _ = Describe("Default tenancy logic", func() {
	var (
		ctx   context.Context
		logic *DefaultTenancyLogic
	)

	BeforeEach(func() {
		var err error

		// Create the context:
		ctx = context.Background()

		// Create the tenancy logic:
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

	Describe("Regular users authenticated with JWT", func() {
		It("Returns the groups as assignable tenants", func() {
			subject := &Subject{
				User:   "my_user",
				Groups: []string{"group1", "group2"},
				Source: SubjectSourceJwt,
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineAssignableTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("group1", "group2"))).To(BeTrue())
		})

		It("Returns the groups as default tenants", func() {
			subject := &Subject{
				User:   "my_user",
				Groups: []string{"group1", "group2"},
				Source: SubjectSourceJwt,
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineDefaultTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("group1", "group2"))).To(BeTrue())
		})

		It("Returns only shared as visible tenants when user has no groups", func() {
			subject := &Subject{
				Source: SubjectSourceJwt,
				User:   "my_user",
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibleTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(SharedTenants)).To(BeTrue())
		})

		It("Returns the groups and shared as visible tenants", func() {
			subject := &Subject{
				User:   "my_user",
				Groups: []string{"group1", "group2"},
				Source: SubjectSourceJwt,
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibleTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(SharedTenants.Union(collections.NewSet("group1", "group2")))).To(BeTrue())
		})
	})

	Describe("Service accounts", func() {
		It("Returns the namespace as the assignable tenant for a service account", func() {
			subject := &Subject{
				User:   "system:serviceaccount:my-ns:my-sa",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineAssignableTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Inclusions()).To(ConsistOf("my-ns"))
		})

		It("Returns the namespace as the default tenant for a service account", func() {
			subject := &Subject{
				User:   "system:serviceaccount:my-ns:my-sa",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineDefaultTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Inclusions()).To(ConsistOf("my-ns"))
		})

		It("Returns the namespace and shared as visible tenants for a service account", func() {
			subject := &Subject{
				User:   "system:serviceaccount:my-ns:my-sa",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibleTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Inclusions()).To(ConsistOf("my-ns", "shared"))
		})

		It("Handles service accounts with different namespaces", func() {
			subject := &Subject{
				Source: SubjectSourceServiceAccount,
				User:   "system:serviceaccount:another-ns:another-sa",
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineDefaultTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Inclusions()).To(ConsistOf("another-ns"))
		})
	})

	Describe("Invalid subject source", func() {
		It("Returns error for unknown source when determining assigned tenants", func() {
			subject := &Subject{
				User:   "my_user",
				Source: SubjectSource("invalid"),
			}
			ctx = ContextWithSubject(ctx, subject)
			_, err := logic.DetermineDefaultTenants(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown subject source"))
			Expect(err.Error()).To(ContainSubstring("invalid"))
		})

		It("Returns error for unknown source when determining visible tenants", func() {
			subject := &Subject{
				User:   "my_user",
				Source: SubjectSource("invalid"),
			}
			ctx = ContextWithSubject(ctx, subject)
			_, err := logic.DetermineVisibleTenants(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown subject source"))
			Expect(err.Error()).To(ContainSubstring("invalid"))
		})

		It("Returns error for empty source when determining assigned tenants", func() {
			subject := &Subject{
				User:   "my_user",
				Source: SubjectSource(""),
			}
			ctx = ContextWithSubject(ctx, subject)
			_, err := logic.DetermineDefaultTenants(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown subject source"))
		})
	})
})
