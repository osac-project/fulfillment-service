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

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	ffv1 "github.com/osac-project/fulfillment-service/internal/api/fulfillment/v1"
	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type HostsServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ ffv1.HostsServer = (*HostsServer)(nil)

type HostsServer struct {
	ffv1.UnimplementedHostsServer

	logger    *slog.Logger
	delegate  privatev1.HostsServer
	inMapper  *GenericMapper[*ffv1.Host, *privatev1.Host]
	outMapper *GenericMapper[*privatev1.Host, *ffv1.Host]
}

func NewHostsServer() *HostsServerBuilder {
	return &HostsServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *HostsServerBuilder) SetLogger(value *slog.Logger) *HostsServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier to use. This is optional.
func (b *HostsServerBuilder) SetNotifier(value *database.Notifier) *HostsServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the attribution logic to use. This is optional.
func (b *HostsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *HostsServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic to use. This is mandatory.
func (b *HostsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *HostsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *HostsServerBuilder) Build() (result *HostsServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Find the full name of the 'status' field so that we can configure the generic server to ignore it. This is
	// because users don't have permission to change the status.
	var object *ffv1.Host
	objectReflect := object.ProtoReflect()
	objectDesc := objectReflect.Descriptor()
	statusField := objectDesc.Fields().ByName("status")
	if statusField == nil {
		err = fmt.Errorf("failed to find the status field of type '%s'", objectDesc.FullName())
		return
	}

	// Create the mappers:
	inMapper, err := NewGenericMapper[*ffv1.Host, *privatev1.Host]().
		SetLogger(b.logger).
		SetStrict(true).
		AddIgnoredFields(statusField.FullName()).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.Host, *ffv1.Host]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	// Create the private server to delegate to:
	delegate, err := NewPrivateHostsServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &HostsServer{
		logger:    b.logger,
		delegate:  delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *HostsServer) List(ctx context.Context,
	request *ffv1.HostsListRequest) (response *ffv1.HostsListResponse, err error) {
	// Create private request with same parameters:
	privateRequest := &privatev1.HostsListRequest{}
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
	publicItems := make([]*ffv1.Host, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &ffv1.Host{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to map private host to public",
				slog.Any("error", err),
			)
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process hosts")
		}
		publicItems[i] = publicItem
	}

	// Create the public response:
	response = &ffv1.HostsListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return
}

func (s *HostsServer) Get(ctx context.Context,
	request *ffv1.HostsGetRequest) (response *ffv1.HostsGetResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.HostsGetRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	privateResponse, err := s.delegate.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateHost := privateResponse.GetObject()
	publicHost := &ffv1.Host{}
	err = s.outMapper.Copy(ctx, privateHost, publicHost)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private host to public",
			slog.Any("error", err),
		)
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process host")
	}

	// Create the public response:
	response = &ffv1.HostsGetResponse{}
	response.SetObject(publicHost)
	return
}

func (s *HostsServer) Create(ctx context.Context,
	request *ffv1.HostsCreateRequest) (response *ffv1.HostsCreateResponse, err error) {
	// Map the public host to private format:
	publicHost := request.GetObject()
	if publicHost == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	privateHost := &privatev1.Host{}
	err = s.inMapper.Copy(ctx, publicHost, privateHost)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public host to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host")
		return
	}

	// Delegate to the private server:
	privateRequest := &privatev1.HostsCreateRequest{}
	privateRequest.SetObject(privateHost)
	privateResponse, err := s.delegate.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	createdPrivateHost := privateResponse.GetObject()
	createdPublicHost := &ffv1.Host{}
	err = s.outMapper.Copy(ctx, createdPrivateHost, createdPublicHost)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private host to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host")
		return
	}

	// Create the public response:
	response = &ffv1.HostsCreateResponse{}
	response.SetObject(createdPublicHost)
	return
}

func (s *HostsServer) Update(ctx context.Context,
	request *ffv1.HostsUpdateRequest) (response *ffv1.HostsUpdateResponse, err error) {
	// Map the public host to private format:
	publicHost := request.GetObject()
	if publicHost == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	privateHost := &privatev1.Host{}
	err = s.inMapper.Copy(ctx, publicHost, privateHost)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public host to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host")
		return
	}

	// Delegate to the private server:
	privateRequest := &privatev1.HostsUpdateRequest{}
	privateRequest.SetObject(privateHost)
	privateRequest.SetUpdateMask(request.GetUpdateMask())
	privateResponse, err := s.delegate.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	updatedPrivateHost := privateResponse.GetObject()
	updatedPublicHost := &ffv1.Host{}
	err = s.outMapper.Copy(ctx, updatedPrivateHost, updatedPublicHost)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private host to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host")
		return
	}

	// Create the public response:
	response = &ffv1.HostsUpdateResponse{}
	response.SetObject(updatedPublicHost)
	return
}

func (s *HostsServer) Delete(ctx context.Context,
	request *ffv1.HostsDeleteRequest) (response *ffv1.HostsDeleteResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.HostsDeleteRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	_, err = s.delegate.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Create the public response:
	response = &ffv1.HostsDeleteResponse{}
	return
}
