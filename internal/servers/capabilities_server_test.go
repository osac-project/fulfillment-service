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
				SetKeycloakUrl("https://my-keycloak.com").
				SetKeycloakRealm("my-realm").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewCapabilitiesServer().
				SetKeycloakUrl("https://my-keycloak.com").
				SetKeycloakRealm("my-realm").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if Keycloak URL is not set", func() {
			server, err := NewCapabilitiesServer().
				SetLogger(logger).
				SetKeycloakRealm("my-realm").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError("keycloak URL is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if Keycloak realm is not set", func() {
			server, err := NewCapabilitiesServer().
				SetLogger(logger).
				SetKeycloakUrl("https://my-keycloak.com").
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError("keycloak realm is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		It("Returns the issuer URL computed from Keycloak URL and realm", func() {
			server, err := NewCapabilitiesServer().
				SetLogger(logger).
				SetKeycloakUrl("https://my-keycloak.com").
				SetKeycloakRealm("my-realm").
				Build()
			Expect(err).ToNot(HaveOccurred())
			response, err := server.Get(ctx, &publicv1.CapabilitiesGetRequest{})
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetAuthn().GetIssuerUrl()).To(Equal("https://my-keycloak.com/realms/my-realm"))
		})

		It("Removes trailing slashes from the Keycloak URL", func() {
			server, err := NewCapabilitiesServer().
				SetLogger(logger).
				SetKeycloakUrl("https://my-keycloak.com///").
				SetKeycloakRealm("my-realm").
				Build()
			Expect(err).ToNot(HaveOccurred())
			response, err := server.Get(ctx, &publicv1.CapabilitiesGetRequest{})
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetAuthn().GetIssuerUrl()).To(Equal("https://my-keycloak.com/realms/my-realm"))
		})
	})
})
