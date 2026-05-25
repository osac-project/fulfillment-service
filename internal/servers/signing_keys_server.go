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
	"errors"
	"log/slog"

	"google.golang.org/genproto/googleapis/api/httpbody"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/token"
)

// SigningKeysServerBuilder builds a SigningKeysServer.
type SigningKeysServerBuilder struct {
	logger *slog.Logger
	sealer *token.Sealer
}

// signingKeysServer implements the SigningKeys gRPC service.
type signingKeysServer struct {
	publicv1.UnimplementedSigningKeysServer
	logger *slog.Logger
	sealer *token.Sealer
}

// NewSigningKeysServer creates a new builder for the signing keys server.
func NewSigningKeysServer() *SigningKeysServerBuilder {
	return &SigningKeysServerBuilder{}
}

func (b *SigningKeysServerBuilder) SetLogger(value *slog.Logger) *SigningKeysServerBuilder {
	b.logger = value
	return b
}

func (b *SigningKeysServerBuilder) SetSealer(value *token.Sealer) *SigningKeysServerBuilder {
	b.sealer = value
	return b
}

func (b *SigningKeysServerBuilder) Build() (publicv1.SigningKeysServer, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	if b.sealer == nil {
		return nil, errors.New("sealer is mandatory")
	}
	return &signingKeysServer{
		logger: b.logger,
		sealer: b.sealer,
	}, nil
}

// Get returns the JSON Web Key Set containing the public signing keys
// used to verify tokens issued by this server.
func (s *signingKeysServer) Get(
	ctx context.Context,
	req *publicv1.SigningKeysGetRequest,
) (*httpbody.HttpBody, error) {
	set, err := s.sealer.JWKSet()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to build JWKS: %v", err)
	}
	data, err := json.Marshal(set)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to encode JWKS: %v", err)
	}
	return &httpbody.HttpBody{
		ContentType: "application/json",
		Data:        data,
	}, nil
}
