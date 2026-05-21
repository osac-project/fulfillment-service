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
	"errors"
	"fmt"
	"log/slog"
	"strings"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

// PrivateCapabilitiesServerBuilder contains the data and logic needed to create a new private capabilities server.
type PrivateCapabilitiesServerBuilder struct {
	logger        *slog.Logger
	keycloakUrl   string
	keycloakRealm string
}

var _ privatev1.CapabilitiesServer = (*PrivateCapabilitiesServer)(nil)

// PrivateCapabilitiesServer is the private server for capabilities, like the issuer URL for authentication.
type PrivateCapabilitiesServer struct {
	privatev1.UnimplementedCapabilitiesServer

	logger    *slog.Logger
	issuerUrl string
}

// NewPrivateCapabilitiesServer creates a builder that can then be used to configure and create a new private
// capabilities server.
func NewPrivateCapabilitiesServer() *PrivateCapabilitiesServerBuilder {
	return &PrivateCapabilitiesServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *PrivateCapabilitiesServerBuilder) SetLogger(value *slog.Logger) *PrivateCapabilitiesServerBuilder {
	b.logger = value
	return b
}

// SetKeycloakUrl sets the base URL of the Keycloak instance.
func (b *PrivateCapabilitiesServerBuilder) SetKeycloakUrl(value string) *PrivateCapabilitiesServerBuilder {
	b.keycloakUrl = value
	return b
}

// SetKeycloakRealm sets the Keycloak realm name used to construct the issuer URL.
func (b *PrivateCapabilitiesServerBuilder) SetKeycloakRealm(value string) *PrivateCapabilitiesServerBuilder {
	b.keycloakRealm = value
	return b
}

// Build uses the data stored in the builder to create a new private capabilities server.
func (b *PrivateCapabilitiesServerBuilder) Build() (result *PrivateCapabilitiesServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.keycloakUrl == "" {
		err = errors.New("keycloak URL is mandatory")
		return
	}
	if b.keycloakRealm == "" {
		err = errors.New("keycloak realm is mandatory")
		return
	}

	// Normalize the Keycloak URL removing any trailing slashes and calculate the issuer URL adding the realm path:
	keycloakUrl := b.keycloakUrl
	for strings.HasSuffix(keycloakUrl, "/") {
		keycloakUrl = strings.TrimSuffix(keycloakUrl, "/")
	}
	issuerUrl := fmt.Sprintf("%s/realms/%s", keycloakUrl, b.keycloakRealm)

	// Create the server:
	result = &PrivateCapabilitiesServer{
		logger:    b.logger,
		issuerUrl: issuerUrl,
	}
	return
}

// Get is the implementation of the method that returns the capabilities of the server.
func (s *PrivateCapabilitiesServer) Get(ctx context.Context,
	request *privatev1.CapabilitiesGetRequest) (response *privatev1.CapabilitiesGetResponse, err error) {
	response = privatev1.CapabilitiesGetResponse_builder{
		Authn: &privatev1.AuthnCapabilities{
			IssuerUrl: s.issuerUrl,
		},
	}.Build()
	return
}
