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

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/events"
)

type OrganizationsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ publicv1.OrganizationsServer = (*OrganizationsServer)(nil)

type OrganizationsServer struct {
	publicv1.UnimplementedOrganizationsServer

	logger    *slog.Logger
	private   privatev1.OrganizationsServer
	inMapper  *GenericMapper[*publicv1.Organization, *privatev1.Organization]
	outMapper *GenericMapper[*privatev1.Organization, *publicv1.Organization]
}

func NewOrganizationsServer() *OrganizationsServerBuilder {
	return &OrganizationsServerBuilder{}
}

func (b *OrganizationsServerBuilder) SetLogger(value *slog.Logger) *OrganizationsServerBuilder {
	b.logger = value
	return b
}

func (b *OrganizationsServerBuilder) SetNotifier(value events.Notifier) *OrganizationsServerBuilder {
	b.notifier = value
	return b
}

func (b *OrganizationsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *OrganizationsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *OrganizationsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *OrganizationsServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *OrganizationsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *OrganizationsServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *OrganizationsServerBuilder) Build() (result *OrganizationsServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the mappers:
	inMapper, err := NewGenericMapper[*publicv1.Organization, *privatev1.Organization]().
		SetLogger(b.logger).
		SetStrict(true).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.Organization, *publicv1.Organization]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	// Create the private server to delegate to:
	delegate, err := NewPrivateOrganizationsServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &OrganizationsServer{
		logger:    b.logger,
		private:   delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *OrganizationsServer) List(ctx context.Context,
	request *publicv1.OrganizationsListRequest) (response *publicv1.OrganizationsListResponse, err error) {
	// Create private request with same parameters:
	privateRequest := &privatev1.OrganizationsListRequest{}
	privateRequest.SetOffset(request.GetOffset())
	privateRequest.SetLimit(request.GetLimit())
	privateRequest.SetFilter(request.GetFilter())

	// Delegate to private server:
	privateResponse, err := s.private.List(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateItems := privateResponse.GetItems()
	publicItems := make([]*publicv1.Organization, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &publicv1.Organization{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to map private organization to public",
				slog.Any("error", err),
			)
			return nil, err
		}
		publicItems[i] = publicItem
	}

	// Build and return the response:
	response = &publicv1.OrganizationsListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return
}

func (s *OrganizationsServer) Get(ctx context.Context,
	request *publicv1.OrganizationsGetRequest) (response *publicv1.OrganizationsGetResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.OrganizationsGetRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	privateResponse, err := s.private.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	publicObject := &publicv1.Organization{}
	err = s.outMapper.Copy(ctx, privateResponse.GetObject(), publicObject)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private organization to public",
			slog.Any("error", err),
		)
		return nil, err
	}

	// Build and return the response:
	response = &publicv1.OrganizationsGetResponse{}
	response.SetObject(publicObject)
	return
}

func (s *OrganizationsServer) Create(ctx context.Context,
	request *publicv1.OrganizationsCreateRequest) (response *publicv1.OrganizationsCreateResponse, err error) {
	// Map public request to private format:
	privateObject := &privatev1.Organization{}
	err = s.inMapper.Copy(ctx, request.GetObject(), privateObject)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public organization to private",
			slog.Any("error", err),
		)
		return nil, err
	}

	// Create private request:
	privateRequest := &privatev1.OrganizationsCreateRequest{}
	privateRequest.SetObject(privateObject)

	// Delegate to private server:
	privateResponse, err := s.private.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	publicObject := &publicv1.Organization{}
	err = s.outMapper.Copy(ctx, privateResponse.GetObject(), publicObject)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private organization to public",
			slog.Any("error", err),
		)
		return nil, err
	}

	// Build and return the response:
	response = &publicv1.OrganizationsCreateResponse{}
	response.SetObject(publicObject)
	return
}

func (s *OrganizationsServer) Update(ctx context.Context,
	request *publicv1.OrganizationsUpdateRequest) (response *publicv1.OrganizationsUpdateResponse, err error) {
	// Map public request to private format:
	privateObject := &privatev1.Organization{}
	err = s.inMapper.Copy(ctx, request.GetObject(), privateObject)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public organization to private",
			slog.Any("error", err),
		)
		return nil, err
	}

	// Create private request:
	privateRequest := &privatev1.OrganizationsUpdateRequest{}
	privateRequest.SetObject(privateObject)
	privateRequest.SetUpdateMask(request.GetUpdateMask())

	// Delegate to private server:
	privateResponse, err := s.private.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	publicObject := &publicv1.Organization{}
	err = s.outMapper.Copy(ctx, privateResponse.GetObject(), publicObject)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private organization to public",
			slog.Any("error", err),
		)
		return nil, err
	}

	// Build and return the response:
	response = &publicv1.OrganizationsUpdateResponse{}
	response.SetObject(publicObject)
	return
}

func (s *OrganizationsServer) Delete(ctx context.Context,
	request *publicv1.OrganizationsDeleteRequest) (response *publicv1.OrganizationsDeleteResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.OrganizationsDeleteRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	_, err = s.private.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Build and return the response:
	response = &publicv1.OrganizationsDeleteResponse{}
	return
}
