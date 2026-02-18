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

type HostPoolsServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ ffv1.HostPoolsServer = (*HostPoolsServer)(nil)

type HostPoolsServer struct {
	ffv1.UnimplementedHostPoolsServer

	logger    *slog.Logger
	delegate  privatev1.HostPoolsServer
	inMapper  *GenericMapper[*ffv1.HostPool, *privatev1.HostPool]
	outMapper *GenericMapper[*privatev1.HostPool, *ffv1.HostPool]
}

func NewHostPoolsServer() *HostPoolsServerBuilder {
	return &HostPoolsServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *HostPoolsServerBuilder) SetLogger(value *slog.Logger) *HostPoolsServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier to use. This is optional.
func (b *HostPoolsServerBuilder) SetNotifier(value *database.Notifier) *HostPoolsServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the attribution logic to use. This is optional.
func (b *HostPoolsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *HostPoolsServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic to use. This is mandatory.
func (b *HostPoolsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *HostPoolsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *HostPoolsServerBuilder) Build() (result *HostPoolsServer, err error) {
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
	var object *ffv1.HostPool
	objectReflect := object.ProtoReflect()
	objectDesc := objectReflect.Descriptor()
	statusField := objectDesc.Fields().ByName("status")
	if statusField == nil {
		err = fmt.Errorf("failed to find the status field of type '%s'", objectDesc.FullName())
		return
	}

	// Create the mappers:
	inMapper, err := NewGenericMapper[*ffv1.HostPool, *privatev1.HostPool]().
		SetLogger(b.logger).
		SetStrict(true).
		AddIgnoredFields(statusField.FullName()).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.HostPool, *ffv1.HostPool]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	// Create the private server to delegate to:
	delegate, err := NewPrivateHostPoolsServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &HostPoolsServer{
		logger:    b.logger,
		delegate:  delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *HostPoolsServer) List(ctx context.Context,
	request *ffv1.HostPoolsListRequest) (response *ffv1.HostPoolsListResponse, err error) {
	// Create private request with same parameters:
	privateRequest := &privatev1.HostPoolsListRequest{}
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
	publicItems := make([]*ffv1.HostPool, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &ffv1.HostPool{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to map private host pool to public",
				slog.Any("error", err),
			)
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process host pools")
		}
		publicItems[i] = publicItem
	}

	// Create the public response:
	response = &ffv1.HostPoolsListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return
}

func (s *HostPoolsServer) Get(ctx context.Context,
	request *ffv1.HostPoolsGetRequest) (response *ffv1.HostPoolsGetResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.HostPoolsGetRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	privateResponse, err := s.delegate.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateHostPool := privateResponse.GetObject()
	publicHostPool := &ffv1.HostPool{}
	err = s.outMapper.Copy(ctx, privateHostPool, publicHostPool)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private host pool to public",
			slog.Any("error", err),
		)
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process host pool")
	}

	// Create the public response:
	response = &ffv1.HostPoolsGetResponse{}
	response.SetObject(publicHostPool)
	return
}

func (s *HostPoolsServer) Create(ctx context.Context,
	request *ffv1.HostPoolsCreateRequest) (response *ffv1.HostPoolsCreateResponse, err error) {
	// Map the public host pool to private format:
	publicHostPool := request.GetObject()
	if publicHostPool == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	privateHostPool := &privatev1.HostPool{}
	err = s.inMapper.Copy(ctx, publicHostPool, privateHostPool)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public host pool to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host pool")
		return
	}

	// Delegate to the private server:
	privateRequest := &privatev1.HostPoolsCreateRequest{}
	privateRequest.SetObject(privateHostPool)
	privateResponse, err := s.delegate.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	createdPrivateHostPool := privateResponse.GetObject()
	createdPublicHostPool := &ffv1.HostPool{}
	err = s.outMapper.Copy(ctx, createdPrivateHostPool, createdPublicHostPool)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private host pool to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host pool")
		return
	}

	// Create the public response:
	response = &ffv1.HostPoolsCreateResponse{}
	response.SetObject(createdPublicHostPool)
	return
}

func (s *HostPoolsServer) Update(ctx context.Context,
	request *ffv1.HostPoolsUpdateRequest) (response *ffv1.HostPoolsUpdateResponse, err error) {
	// Map the public host pool to private format:
	publicHostPool := request.GetObject()
	if publicHostPool == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	privateHostPool := &privatev1.HostPool{}
	err = s.inMapper.Copy(ctx, publicHostPool, privateHostPool)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public host pool to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host pool")
		return
	}

	// Delegate to the private server:
	privateRequest := &privatev1.HostPoolsUpdateRequest{}
	privateRequest.SetObject(privateHostPool)
	privateRequest.SetUpdateMask(request.GetUpdateMask())
	privateResponse, err := s.delegate.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	updatedPrivateHostPool := privateResponse.GetObject()
	updatedPublicHostPool := &ffv1.HostPool{}
	err = s.outMapper.Copy(ctx, updatedPrivateHostPool, updatedPublicHostPool)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private host pool to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host pool")
		return
	}

	// Create the public response:
	response = &ffv1.HostPoolsUpdateResponse{}
	response.SetObject(updatedPublicHostPool)
	return
}

func (s *HostPoolsServer) Delete(ctx context.Context,
	request *ffv1.HostPoolsDeleteRequest) (response *ffv1.HostPoolsDeleteResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.HostPoolsDeleteRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	_, err = s.delegate.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Create the public response:
	response = &ffv1.HostPoolsDeleteResponse{}
	return
}
