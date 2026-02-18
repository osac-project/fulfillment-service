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
	"log/slog"
	"slices"

	metadatav1 "github.com/osac-project/fulfillment-service/internal/api/metadata/v1"
)

// MetadataServerBuilder Contains the data and logic needed to create a new metadata server.
type MetadataServerBuilder struct {
	logger                   *slog.Logger
	authnTrustedTokenIssuers []string
}

// Make sure that we implement the interface:
var _ metadatav1.MetadataServer = (*MetadataServer)(nil)

// MetadataServer is the server for metadata, like the list of trusted access token issuers.
type MetadataServer struct {
	metadatav1.UnimplementedMetadataServer

	logger                   *slog.Logger
	authnTrustedTokenIssuers []string
}

// NewMetadataServer creates a builder that can the be used to configure and create a new metadata server.
func NewMetadataServer() *MetadataServerBuilder {
	return &MetadataServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *MetadataServerBuilder) SetLogger(value *slog.Logger) *MetadataServerBuilder {
	b.logger = value
	return b
}

// AddAutnTrustedTokenIssuers adds a list of token issuers whose tokens are accepted by the server for authentication.
func (b *MetadataServerBuilder) AddAutnTrustedTokenIssuers(value ...string) *MetadataServerBuilder {
	b.authnTrustedTokenIssuers = append(b.authnTrustedTokenIssuers, value...)
	return b
}

// Build uses the data stored in the builder to create a new metadata server.
func (b *MetadataServerBuilder) Build() (result *MetadataServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Make sure that the list of issuers doens't have duplicates, and that it is sorted in a predictable way:
	authnTrustedTokenIssuers := slices.Clone(b.authnTrustedTokenIssuers)
	slices.Sort(authnTrustedTokenIssuers)
	authnTrustedTokenIssuers = slices.Compact(authnTrustedTokenIssuers)

	// Create and populate the object:
	result = &MetadataServer{
		logger:                   b.logger,
		authnTrustedTokenIssuers: authnTrustedTokenIssuers,
	}
	return
}

// Get is the implementation of the method that returns the metadata of the server.
func (s *MetadataServer) Get(ctx context.Context,
	request *metadatav1.MetadataGetRequest) (response *metadatav1.MetadataGetResponse, err error) {
	response = metadatav1.MetadataGetResponse_builder{
		Authn: &metadatav1.Authn{
			TrustedTokenIssuers: s.authnTrustedTokenIssuers,
		},
	}.Build()
	return
}
