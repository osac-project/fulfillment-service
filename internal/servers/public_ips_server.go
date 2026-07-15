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
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/events"
)

type PublicIPsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ publicv1.PublicIPsServer = (*PublicIPsServer)(nil)

type PublicIPsServer struct {
	publicv1.UnimplementedPublicIPsServer

	logger    *slog.Logger
	delegate  privatev1.PublicIPsServer
	inMapper  *GenericMapper[*publicv1.PublicIP, *privatev1.PublicIP]
	outMapper *GenericMapper[*privatev1.PublicIP, *publicv1.PublicIP]
}

func NewPublicIPsServer() *PublicIPsServerBuilder {
	return &PublicIPsServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *PublicIPsServerBuilder) SetLogger(value *slog.Logger) *PublicIPsServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier to use. This is optional.
func (b *PublicIPsServerBuilder) SetNotifier(value events.Notifier) *PublicIPsServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the attribution logic to use. This is mandatory.
func (b *PublicIPsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PublicIPsServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic to use. This is mandatory.
func (b *PublicIPsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PublicIPsServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PublicIPsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PublicIPsServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PublicIPsServerBuilder) Build() (result *PublicIPsServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return result, err
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return result, err
	}
	if b.attributionLogic == nil {
		err = errors.New("attribution logic is mandatory")
		return result, err
	}

	// Create the mappers:
	inMapper, err := NewGenericMapper[*publicv1.PublicIP, *privatev1.PublicIP]().
		SetLogger(b.logger).
		SetStrict(true).
		Build()
	if err != nil {
		return result, err
	}
	outMapper, err := NewGenericMapper[*privatev1.PublicIP, *publicv1.PublicIP]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return result, err
	}

	// Create the private server to delegate to:
	delegate, err := NewPrivatePublicIPsServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return result, err
	}

	// Create and populate the object:
	result = &PublicIPsServer{
		logger:    b.logger,
		delegate:  delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return result, err
}

func (s *PublicIPsServer) List(ctx context.Context,
	request *publicv1.PublicIPsListRequest) (response *publicv1.PublicIPsListResponse, err error) {
	// Create private request with same parameters:
	privateRequest := &privatev1.PublicIPsListRequest{}
	privateRequest.SetOffset(request.GetOffset())
	privateRequest.SetLimit(request.GetLimit())
	privateRequest.SetFilter(request.GetFilter())
	privateRequest.SetOrder(request.GetOrder())

	// Delegate to private server:
	privateResponse, err := s.delegate.List(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateItems := privateResponse.GetItems()
	publicItems := make([]*publicv1.PublicIP, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &publicv1.PublicIP{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to map private public IP to public",
				slog.Any("error", err),
			)
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process public IPs")
		}
		publicItems[i] = publicItem
	}

	// Create the public response:
	response = &publicv1.PublicIPsListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return response, err
}

func (s *PublicIPsServer) Get(ctx context.Context,
	request *publicv1.PublicIPsGetRequest) (response *publicv1.PublicIPsGetResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.PublicIPsGetRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	privateResponse, err := s.delegate.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privatePublicIP := privateResponse.GetObject()
	publicPublicIP := &publicv1.PublicIP{}
	err = s.outMapper.Copy(ctx, privatePublicIP, publicPublicIP)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private public IP to public",
			slog.Any("error", err),
		)
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP")
	}

	// Create the public response:
	response = &publicv1.PublicIPsGetResponse{}
	response.SetObject(publicPublicIP)
	return response, err
}

func (s *PublicIPsServer) Create(ctx context.Context,
	request *publicv1.PublicIPsCreateRequest) (response *publicv1.PublicIPsCreateResponse, err error) {
	// Map the public IP to private format:
	publicPublicIP := request.GetObject()
	if publicPublicIP == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return response, err
	}
	privatePublicIP := &privatev1.PublicIP{}
	err = s.inMapper.Copy(ctx, publicPublicIP, privatePublicIP)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public IP to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP")
		return response, err
	}

	// Delegate to the private server:
	privateRequest := &privatev1.PublicIPsCreateRequest{}
	privateRequest.SetObject(privatePublicIP)
	privateResponse, err := s.delegate.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	createdPrivatePublicIP := privateResponse.GetObject()
	createdPublicPublicIP := &publicv1.PublicIP{}
	err = s.outMapper.Copy(ctx, createdPrivatePublicIP, createdPublicPublicIP)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private public IP to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP")
		return response, err
	}

	// Create the public response:
	response = &publicv1.PublicIPsCreateResponse{}
	response.SetObject(createdPublicPublicIP)
	return response, err
}

func (s *PublicIPsServer) Update(ctx context.Context,
	request *publicv1.PublicIPsUpdateRequest) (response *publicv1.PublicIPsUpdateResponse, err error) {
	// Map the public IP to private format:
	publicPublicIP := request.GetObject()
	if publicPublicIP == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return response, err
	}
	privatePublicIP := &privatev1.PublicIP{}
	err = s.inMapper.Copy(ctx, publicPublicIP, privatePublicIP)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public IP to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP")
		return response, err
	}

	// Delegate to the private server:
	privateRequest := &privatev1.PublicIPsUpdateRequest{}
	privateRequest.SetObject(privatePublicIP)
	privateRequest.SetUpdateMask(request.GetUpdateMask())
	privateRequest.SetLock(request.GetLock())
	privateResponse, err := s.delegate.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	updatedPrivatePublicIP := privateResponse.GetObject()
	updatedPublicPublicIP := &publicv1.PublicIP{}
	err = s.outMapper.Copy(ctx, updatedPrivatePublicIP, updatedPublicPublicIP)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private public IP to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP")
		return response, err
	}

	// Create the public response:
	response = &publicv1.PublicIPsUpdateResponse{}
	response.SetObject(updatedPublicPublicIP)
	return response, err
}

func (s *PublicIPsServer) Delete(ctx context.Context,
	request *publicv1.PublicIPsDeleteRequest) (response *publicv1.PublicIPsDeleteResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.PublicIPsDeleteRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	_, err = s.delegate.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Create the public response:
	response = &publicv1.PublicIPsDeleteResponse{}
	return response, err
}
