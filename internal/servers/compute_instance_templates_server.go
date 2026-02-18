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

	ffv1 "github.com/osac-project/fulfillment-service/internal/api/fulfillment/v1"
	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type ComputeInstanceTemplatesServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ ffv1.ComputeInstanceTemplatesServer = (*ComputeInstanceTemplatesServer)(nil)

type ComputeInstanceTemplatesServer struct {
	ffv1.UnimplementedComputeInstanceTemplatesServer

	logger    *slog.Logger
	delegate  privatev1.ComputeInstanceTemplatesServer
	inMapper  *GenericMapper[*ffv1.ComputeInstanceTemplate, *privatev1.ComputeInstanceTemplate]
	outMapper *GenericMapper[*privatev1.ComputeInstanceTemplate, *ffv1.ComputeInstanceTemplate]
}

func NewComputeInstanceTemplatesServer() *ComputeInstanceTemplatesServerBuilder {
	return &ComputeInstanceTemplatesServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *ComputeInstanceTemplatesServerBuilder) SetLogger(value *slog.Logger) *ComputeInstanceTemplatesServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier to use. This is optional.
func (b *ComputeInstanceTemplatesServerBuilder) SetNotifier(value *database.Notifier) *ComputeInstanceTemplatesServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the attribution logic to use. This is optional.
func (b *ComputeInstanceTemplatesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *ComputeInstanceTemplatesServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic to use. This is mandatory.
func (b *ComputeInstanceTemplatesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *ComputeInstanceTemplatesServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *ComputeInstanceTemplatesServerBuilder) Build() (result *ComputeInstanceTemplatesServer, err error) {
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
	inMapper, err := NewGenericMapper[*ffv1.ComputeInstanceTemplate, *privatev1.ComputeInstanceTemplate]().
		SetLogger(b.logger).
		SetStrict(true).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.ComputeInstanceTemplate, *ffv1.ComputeInstanceTemplate]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	// Create the private server to delegate to:
	delegate, err := NewPrivateComputeInstanceTemplatesServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &ComputeInstanceTemplatesServer{
		logger:    b.logger,
		delegate:  delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *ComputeInstanceTemplatesServer) List(ctx context.Context,
	request *ffv1.ComputeInstanceTemplatesListRequest) (response *ffv1.ComputeInstanceTemplatesListResponse, err error) {
	// Create private request with same parameters:
	privateRequest := &privatev1.ComputeInstanceTemplatesListRequest{}
	privateRequest.SetOffset(request.GetOffset())
	privateRequest.SetLimit(request.GetLimit())
	privateRequest.SetFilter(request.GetFilter())

	// Delegate to private server:
	privateResponse, err := s.delegate.List(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateItems := privateResponse.GetItems()
	publicItems := make([]*ffv1.ComputeInstanceTemplate, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &ffv1.ComputeInstanceTemplate{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to map private compute instance template to public",
				slog.Any("error", err),
			)
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance templates")
		}
		publicItems[i] = publicItem
	}

	// Create the public response:
	response = &ffv1.ComputeInstanceTemplatesListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return
}

func (s *ComputeInstanceTemplatesServer) Get(ctx context.Context,
	request *ffv1.ComputeInstanceTemplatesGetRequest) (response *ffv1.ComputeInstanceTemplatesGetResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.ComputeInstanceTemplatesGetRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	privateResponse, err := s.delegate.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateComputeInstanceTemplate := privateResponse.GetObject()
	publicComputeInstanceTemplate := &ffv1.ComputeInstanceTemplate{}
	err = s.outMapper.Copy(ctx, privateComputeInstanceTemplate, publicComputeInstanceTemplate)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private compute instance template to public",
			slog.Any("error", err),
		)
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance template")
	}

	// Create the public response:
	response = &ffv1.ComputeInstanceTemplatesGetResponse{}
	response.SetObject(publicComputeInstanceTemplate)
	return
}

func (s *ComputeInstanceTemplatesServer) Create(ctx context.Context,
	request *ffv1.ComputeInstanceTemplatesCreateRequest) (response *ffv1.ComputeInstanceTemplatesCreateResponse, err error) {
	// Map the public compute instance template to private format:
	publicComputeInstanceTemplate := request.GetObject()
	if publicComputeInstanceTemplate == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	privateComputeInstanceTemplate := &privatev1.ComputeInstanceTemplate{}
	err = s.inMapper.Copy(ctx, publicComputeInstanceTemplate, privateComputeInstanceTemplate)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public compute instance template to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance template")
		return
	}

	// Delegate to the private server:
	privateRequest := &privatev1.ComputeInstanceTemplatesCreateRequest{}
	privateRequest.SetObject(privateComputeInstanceTemplate)
	privateResponse, err := s.delegate.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	createdPrivateComputeInstanceTemplate := privateResponse.GetObject()
	createdPublicComputeInstanceTemplate := &ffv1.ComputeInstanceTemplate{}
	err = s.outMapper.Copy(ctx, createdPrivateComputeInstanceTemplate, createdPublicComputeInstanceTemplate)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private compute instance template to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance template")
		return
	}

	// Create the public response:
	response = &ffv1.ComputeInstanceTemplatesCreateResponse{}
	response.SetObject(createdPublicComputeInstanceTemplate)
	return
}

func (s *ComputeInstanceTemplatesServer) Update(ctx context.Context,
	request *ffv1.ComputeInstanceTemplatesUpdateRequest) (response *ffv1.ComputeInstanceTemplatesUpdateResponse, err error) {
	// Validate the request:
	publicComputeInstanceTemplate := request.GetObject()
	if publicComputeInstanceTemplate == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	id := publicComputeInstanceTemplate.GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	// Get the existing object from the private server:
	getRequest := &privatev1.ComputeInstanceTemplatesGetRequest{}
	getRequest.SetId(id)
	getResponse, err := s.delegate.Get(ctx, getRequest)
	if err != nil {
		return nil, err
	}
	existingPrivateComputeInstanceTemplate := getResponse.GetObject()

	// Map the public changes to the existing private object (preserving private data):
	err = s.inMapper.Copy(ctx, publicComputeInstanceTemplate, existingPrivateComputeInstanceTemplate)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public compute instance template to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance template")
		return
	}

	// Delegate to the private server with the merged object:
	privateRequest := &privatev1.ComputeInstanceTemplatesUpdateRequest{}
	privateRequest.SetObject(existingPrivateComputeInstanceTemplate)
	privateResponse, err := s.delegate.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	updatedPrivateComputeInstanceTemplate := privateResponse.GetObject()
	updatedPublicComputeInstanceTemplate := &ffv1.ComputeInstanceTemplate{}
	err = s.outMapper.Copy(ctx, updatedPrivateComputeInstanceTemplate, updatedPublicComputeInstanceTemplate)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private compute instance template to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance template")
		return
	}

	// Create the public response:
	response = &ffv1.ComputeInstanceTemplatesUpdateResponse{}
	response.SetObject(updatedPublicComputeInstanceTemplate)
	return
}

func (s *ComputeInstanceTemplatesServer) Delete(ctx context.Context,
	request *ffv1.ComputeInstanceTemplatesDeleteRequest) (response *ffv1.ComputeInstanceTemplatesDeleteResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.ComputeInstanceTemplatesDeleteRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	_, err = s.delegate.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Create the public response:
	response = &ffv1.ComputeInstanceTemplatesDeleteResponse{}
	return
}
