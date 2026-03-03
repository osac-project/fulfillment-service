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

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type HostClassesServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ publicv1.HostClassesServer = (*HostClassesServer)(nil)

type HostClassesServer struct {
	publicv1.UnimplementedHostClassesServer

	logger    *slog.Logger
	delegate  privatev1.HostClassesServer
	inMapper  *GenericMapper[*publicv1.HostClass, *privatev1.HostClass]
	outMapper *GenericMapper[*privatev1.HostClass, *publicv1.HostClass]
}

func NewHostClassesServer() *HostClassesServerBuilder {
	return &HostClassesServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *HostClassesServerBuilder) SetLogger(value *slog.Logger) *HostClassesServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier to use. This is optional.
func (b *HostClassesServerBuilder) SetNotifier(value *database.Notifier) *HostClassesServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the attribution logic to use. This is optional.
func (b *HostClassesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *HostClassesServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic to use. This is mandatory.
func (b *HostClassesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *HostClassesServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *HostClassesServerBuilder) Build() (result *HostClassesServer, err error) {
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
	inMapper, err := NewGenericMapper[*publicv1.HostClass, *privatev1.HostClass]().
		SetLogger(b.logger).
		SetStrict(true).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.HostClass, *publicv1.HostClass]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	// Create the private server to delegate to:
	delegate, err := NewPrivateHostClassesServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &HostClassesServer{
		logger:    b.logger,
		delegate:  delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *HostClassesServer) List(ctx context.Context,
	request *publicv1.HostClassesListRequest) (response *publicv1.HostClassesListResponse, err error) {
	// Create private request with same parameters:
	privateRequest := &privatev1.HostClassesListRequest{}
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
	publicItems := make([]*publicv1.HostClass, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &publicv1.HostClass{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to map private host class to public",
				slog.Any("error", err),
			)
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process host classes")
		}
		publicItems[i] = publicItem
	}

	// Create the public response:
	response = &publicv1.HostClassesListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return
}

func (s *HostClassesServer) Get(ctx context.Context,
	request *publicv1.HostClassesGetRequest) (response *publicv1.HostClassesGetResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.HostClassesGetRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	privateResponse, err := s.delegate.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateHostClass := privateResponse.GetObject()
	publicHostClass := &publicv1.HostClass{}
	err = s.outMapper.Copy(ctx, privateHostClass, publicHostClass)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private host class to public",
			slog.Any("error", err),
		)
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process host class")
	}

	// Create the public response:
	response = &publicv1.HostClassesGetResponse{}
	response.SetObject(publicHostClass)
	return
}

func (s *HostClassesServer) Create(ctx context.Context,
	request *publicv1.HostClassesCreateRequest) (response *publicv1.HostClassesCreateResponse, err error) {
	// Map the public host class to private format:
	publicHostClass := request.GetObject()
	if publicHostClass == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	privateHostClass := &privatev1.HostClass{}
	err = s.inMapper.Copy(ctx, publicHostClass, privateHostClass)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public host class to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host class")
		return
	}

	// Delegate to the private server:
	privateRequest := &privatev1.HostClassesCreateRequest{}
	privateRequest.SetObject(privateHostClass)
	privateResponse, err := s.delegate.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	createdPrivateHostClass := privateResponse.GetObject()
	createdPublicHostClass := &publicv1.HostClass{}
	err = s.outMapper.Copy(ctx, createdPrivateHostClass, createdPublicHostClass)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private host class to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host class")
		return
	}

	// Create the public response:
	response = &publicv1.HostClassesCreateResponse{}
	response.SetObject(createdPublicHostClass)
	return
}

func (s *HostClassesServer) Update(ctx context.Context,
	request *publicv1.HostClassesUpdateRequest) (response *publicv1.HostClassesUpdateResponse, err error) {
	// Validate the request:
	publicHostClass := request.GetObject()
	if publicHostClass == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	id := publicHostClass.GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	// Get the existing object from the private server:
	getRequest := &privatev1.HostClassesGetRequest{}
	getRequest.SetId(id)
	getResponse, err := s.delegate.Get(ctx, getRequest)
	if err != nil {
		return nil, err
	}
	existingPrivateHostClass := getResponse.GetObject()

	// Map the public changes to the existing private object (preserving private data):
	err = s.inMapper.Copy(ctx, publicHostClass, existingPrivateHostClass)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public host class to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host class")
		return
	}

	// Delegate to the private server with the merged object:
	privateRequest := &privatev1.HostClassesUpdateRequest{}
	privateRequest.SetObject(existingPrivateHostClass)
	privateResponse, err := s.delegate.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	updatedPrivateHostClass := privateResponse.GetObject()
	updatedPublicHostClass := &publicv1.HostClass{}
	err = s.outMapper.Copy(ctx, updatedPrivateHostClass, updatedPublicHostClass)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private host class to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process host class")
		return
	}

	// Create the public response:
	response = &publicv1.HostClassesUpdateResponse{}
	response.SetObject(updatedPublicHostClass)
	return
}

func (s *HostClassesServer) Delete(ctx context.Context,
	request *publicv1.HostClassesDeleteRequest) (response *publicv1.HostClassesDeleteResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.HostClassesDeleteRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	_, err = s.delegate.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Create the public response:
	response = &publicv1.HostClassesDeleteResponse{}
	return
}
