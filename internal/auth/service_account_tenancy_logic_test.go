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

var _ = Describe("Service account tenancy logic", func() {
	var (
		ctx   context.Context
		logic *ServiceAccountTenancyLogic
	)

	BeforeEach(func() {
		var err error

		// Create the context:
		ctx = context.Background()

		// Create the tenancy logic:
		logic, err = NewServiceAccountTenancyLogic().
			SetLogger(logger).
			Build()
		Expect(err).ToNot(HaveOccurred())
		Expect(logic).ToNot(BeNil())
	})

	Describe("Builder", func() {
		It("Fails if logger is not set", func() {
			logic, err := NewServiceAccountTenancyLogic().
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger is mandatory"))
			Expect(logic).To(BeNil())
		})
	})

	Describe("Determine assignable tenants", func() {
		It("Returns the namespace for a valid service account", func() {
			subject := &Subject{
				User:   "system:serviceaccount:my-ns:my-sa",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineAssignableTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("my-ns"))).To(BeTrue())
		})

		It("Fails if the subject is not a service account", func() {
			subject := &Subject{
				User:   "my_user",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			_, err := logic.DetermineAssignableTenants(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a service account"))
			Expect(err.Error()).To(ContainSubstring("system:serviceaccount:"))
		})

		It("Fails if the subject has the wrong prefix", func() {
			subject := &Subject{
				User:   "system:junk:my-ns:my-sa",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			_, err := logic.DetermineAssignableTenants(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a service account"))
		})

		It("Fails if the subject has the wrong number of parts", func() {
			subject := &Subject{
				User:   "system:serviceaccount:my-ns:my-sa:junk",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			_, err := logic.DetermineAssignableTenants(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a service account"))
		})
	})

	Describe("Determine default tenants", func() {
		It("Returns the namespace for a valid service account", func() {
			subject := &Subject{
				User:   "system:serviceaccount:my-ns:my-sa",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineDefaultTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("my-ns"))).To(BeTrue())
		})

		It("Fails if the subject is not a service account", func() {
			subject := &Subject{
				User:   "my_user",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			_, err := logic.DetermineDefaultTenants(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a service account"))
			Expect(err.Error()).To(ContainSubstring("system:serviceaccount:"))
		})

		It("Fails if the subject has the wrong prefix", func() {
			subject := &Subject{
				User:   "system:junk:my-ns:my-sa",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			_, err := logic.DetermineDefaultTenants(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a service account"))
		})

		It("Fails if the subject has the wrong number of parts", func() {
			subject := &Subject{
				User:   "system:serviceaccount:my-ns:my-sa:junk",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			_, err := logic.DetermineDefaultTenants(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a service account"))
		})
	})

	Describe("Determine visible tenants", func() {
		It("Returns the namespace and shared for a valid service account", func() {
			subject := &Subject{
				User:   "system:serviceaccount:my-ns:my-sa",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			result, err := logic.DetermineVisibleTenants(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Equal(collections.NewSet("my-ns", "shared"))).To(BeTrue())
		})

		It("Fails if the subject is not a service account", func() {
			subject := &Subject{
				User:   "regular_user",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			_, err := logic.DetermineVisibleTenants(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a service account"))
		})

		It("Fails if the subject has the wrong number of parts", func() {
			subject := &Subject{
				User:   "system:serviceaccount:my-ns:my-sa:junk",
				Source: SubjectSourceServiceAccount,
			}
			ctx = ContextWithSubject(ctx, subject)
			_, err := logic.DetermineVisibleTenants(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not a service account"))
		})
	})
})
