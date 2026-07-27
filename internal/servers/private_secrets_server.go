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
	"context"
	"errors"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/events"
)

type PrivateSecretsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.SecretsServer = (*PrivateSecretsServer)(nil)

type PrivateSecretsServer struct {
	privatev1.UnimplementedSecretsServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.Secret]
}

func NewPrivateSecretsServer() *PrivateSecretsServerBuilder {
	return &PrivateSecretsServerBuilder{}
}

func (b *PrivateSecretsServerBuilder) SetLogger(value *slog.Logger) *PrivateSecretsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateSecretsServerBuilder) SetNotifier(value events.Notifier) *PrivateSecretsServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateSecretsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateSecretsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateSecretsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateSecretsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateSecretsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateSecretsServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateSecretsServerBuilder) Build() (result *PrivateSecretsServer, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	s := &PrivateSecretsServer{
		logger: b.logger,
	}

	s.generic, err = NewGenericServer[*privatev1.Secret]().
		SetLogger(b.logger).
		SetService(privatev1.Secrets_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetRedactFunc(s.redact).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	result = s
	return
}

func (s *PrivateSecretsServer) redact(object *privatev1.Secret) *privatev1.Secret {
	if spec := object.GetSpec(); spec != nil {
		spec.SetData(nil)
	}
	if status := object.GetStatus(); status != nil {
		status.SetResolvedData(nil)
	}
	return object
}

// List fetches a list of secret objects from postgres.
// Secret data itself should never be included in the response, and users should use Get
// to fetch individual secrets with populated status.resolved_data.
func (s *PrivateSecretsServer) List(ctx context.Context,
	request *privatev1.SecretsListRequest) (response *privatev1.SecretsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateSecretsServer) Get(ctx context.Context,
	request *privatev1.SecretsGetRequest) (response *privatev1.SecretsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)

	// TODO: Fetch secret data from the storage backend.

	return
}

func (s *PrivateSecretsServer) Create(ctx context.Context,
	request *privatev1.SecretsCreateRequest) (response *privatev1.SecretsCreateResponse, err error) {

	secret := request.GetObject()

	err = s.validateSecretCreate(secret)
	if err != nil {
		return
	}

	secret.SetId("")

	// Default unspecified backend to Vault.
	if secret.GetSpec().GetBackend() == privatev1.SecretBackend_SECRET_BACKEND_UNSPECIFIED {
		secret.GetSpec().SetBackend(privatev1.SecretBackend_SECRET_BACKEND_VAULT)
	}

	// TODO: Store secret data in the storage backend.

	err = s.generic.Create(ctx, request, &response)

	return
}

func (s *PrivateSecretsServer) Update(ctx context.Context,
	request *privatev1.SecretsUpdateRequest) (response *privatev1.SecretsUpdateResponse, err error) {
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	getRequest := &privatev1.SecretsGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.SecretsGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	existingSecret := getResponse.GetObject()

	err = s.validateSecretUpdate(ctx, request.GetObject(), existingSecret)
	if err != nil {
		return
	}

	// TODO: Update secret data in the storage backend.

	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateSecretsServer) Delete(ctx context.Context,
	request *privatev1.SecretsDeleteRequest) (response *privatev1.SecretsDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateSecretsServer) Signal(ctx context.Context,
	request *privatev1.SecretsSignalRequest) (response *privatev1.SecretsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

func (s *PrivateSecretsServer) validateSecretCreate(secret *privatev1.Secret) error {
	if secret == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "secret is mandatory")
	}
	if secret.GetMetadata() == nil || secret.GetMetadata().GetName() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'metadata.name' is required")
	}
	spec := secret.GetSpec()
	if spec == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec' is required")
	}

	switch spec.GetBackend() {
	case privatev1.SecretBackend_SECRET_BACKEND_HUB:
		if err := s.validateHubSecretCreate(spec); err != nil {
			return err
		}
	default:
		if len(spec.GetData()) == 0 {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.data' is required")
		}
	}

	return nil
}

// Hub secret data is created by external sources and fully managed elsewhere - the Secret type
// only manages the metadata and provides utility for fetching the secret data and
// is not the source of truth for the data itself - hence why we reject spec.data
func (s *PrivateSecretsServer) validateHubSecretCreate(spec *privatev1.SecretSpec) error {
	if len(spec.GetCoordinates()) == 0 {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.coordinates' is required when backend is HUB")
	}
	if len(spec.GetData()) > 0 {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.data' must be empty when backend is HUB")
	}
	return nil
}

func (s *PrivateSecretsServer) validateSecretUpdate(_ context.Context,
	newSecret *privatev1.Secret, existingSecret *privatev1.Secret) error {
	if newSecret.GetSpec().GetBackend() != privatev1.SecretBackend_SECRET_BACKEND_UNSPECIFIED &&
		newSecret.GetSpec().GetBackend() != existingSecret.GetSpec().GetBackend() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.backend' is immutable and cannot be changed from '%s' to '%s'",
			existingSecret.GetSpec().GetBackend(), newSecret.GetSpec().GetBackend())
	}
	if existingSecret.GetSpec().GetBackend() == privatev1.SecretBackend_SECRET_BACKEND_HUB &&
		len(newSecret.GetSpec().GetData()) > 0 {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.data' must be empty when backend is HUB")
	}
	return nil
}
