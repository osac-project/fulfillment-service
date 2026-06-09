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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
)

var _ = Describe("Capabilities server", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewCapabilitiesServer().
				SetLogger(logger).
				AddAuthnTrustedTokenIssuers(TokenIssuerPair{
					InternalURL: "https://internal-issuer.com",
					ExternalURL: "https://external-issuer.com",
				}).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewCapabilitiesServer().
				AddAuthnTrustedTokenIssuers(TokenIssuerPair{
					InternalURL: "https://internal-issuer.com",
					ExternalURL: "https://external-issuer.com",
				}).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		It("Returns no token issuers when none are configured", func() {
			server, err := NewCapabilitiesServer().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			response, err := server.Get(ctx, &publicv1.CapabilitiesGetRequest{})
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetAuthn().GetTrustedTokenIssuers()).To(BeEmpty())
		})

		It("Returns one token issuer with internal and external URLs", func() {
			server, err := NewCapabilitiesServer().
				SetLogger(logger).
				AddAuthnTrustedTokenIssuers(TokenIssuerPair{
					InternalURL: "https://internal-issuer.com",
					ExternalURL: "https://external-issuer.com",
				}).
				Build()
			Expect(err).ToNot(HaveOccurred())
			response, err := server.Get(ctx, &publicv1.CapabilitiesGetRequest{})
			Expect(err).ToNot(HaveOccurred())
			issuers := response.GetAuthn().GetTrustedTokenIssuers()
			Expect(issuers).To(HaveLen(1))
			Expect(issuers[0].GetInternalUrl()).To(Equal("https://internal-issuer.com"))
			Expect(issuers[0].GetExternalUrl()).To(Equal("https://external-issuer.com"))
		})

		It("Returns multiple token issuers in order", func() {
			server, err := NewCapabilitiesServer().
				SetLogger(logger).
				AddAuthnTrustedTokenIssuers(
					TokenIssuerPair{
						InternalURL: "https://internal-a.com",
						ExternalURL: "https://external-a.com",
					},
					TokenIssuerPair{
						InternalURL: "https://internal-b.com",
						ExternalURL: "https://external-b.com",
					},
				).
				Build()
			Expect(err).ToNot(HaveOccurred())
			response, err := server.Get(ctx, &publicv1.CapabilitiesGetRequest{})
			Expect(err).ToNot(HaveOccurred())
			issuers := response.GetAuthn().GetTrustedTokenIssuers()
			Expect(issuers).To(HaveLen(2))
			Expect(issuers[0].GetInternalUrl()).To(Equal("https://internal-a.com"))
			Expect(issuers[0].GetExternalUrl()).To(Equal("https://external-a.com"))
			Expect(issuers[1].GetInternalUrl()).To(Equal("https://internal-b.com"))
			Expect(issuers[1].GetExternalUrl()).To(Equal("https://external-b.com"))
		})
	})
})
