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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
)

var _ = Describe("Identity Providers Server", func() {
	var (
		ctrl         *gomock.Controller
		tenancyLogic *auth.MockTenancyLogic
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		tenancyLogic = auth.NewMockTenancyLogic(ctrl)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("Builder", func() {
		It("builds successfully with all required parameters", func() {
			attributionLogic := auth.NewMockAttributionLogic(ctrl)
			server, err := NewIdentityProvidersServer().
				SetLogger(logger).
				SetTenancyLogic(tenancyLogic).
				SetAttributionLogic(attributionLogic).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("fails if logger is not set", func() {
			server, err := NewIdentityProvidersServer().
				SetTenancyLogic(tenancyLogic).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("fails if tenancy logic is not set", func() {
			server, err := NewIdentityProvidersServer().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Interface compliance", func() {
		It("implements IdentityProvidersServer interface", func() {
			var _ publicv1.IdentityProvidersServer = (*IdentityProvidersServer)(nil)
		})
	})
})
