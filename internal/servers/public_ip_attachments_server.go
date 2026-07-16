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

type PublicIPAttachmentsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ publicv1.PublicIPAttachmentsServer = (*PublicIPAttachmentsServer)(nil)

type PublicIPAttachmentsServer struct {
	publicv1.UnimplementedPublicIPAttachmentsServer

	logger    *slog.Logger
	delegate  privatev1.PublicIPAttachmentsServer
	inMapper  *GenericMapper[*publicv1.PublicIPAttachment, *privatev1.PublicIPAttachment]
	outMapper *GenericMapper[*privatev1.PublicIPAttachment, *publicv1.PublicIPAttachment]
}

func NewPublicIPAttachmentsServer() *PublicIPAttachmentsServerBuilder {
	return &PublicIPAttachmentsServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *PublicIPAttachmentsServerBuilder) SetLogger(value *slog.Logger) *PublicIPAttachmentsServerBuilder {
	b.logger = value
	return b
}

// SetAttributionLogic sets the attribution logic to use. This is mandatory.
func (b *PublicIPAttachmentsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PublicIPAttachmentsServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic to use. This is mandatory.
func (b *PublicIPAttachmentsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PublicIPAttachmentsServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetNotifier sets the notifier to use. This is optional.
func (b *PublicIPAttachmentsServerBuilder) SetNotifier(value events.Notifier) *PublicIPAttachmentsServerBuilder {
	b.notifier = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PublicIPAttachmentsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PublicIPAttachmentsServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PublicIPAttachmentsServerBuilder) Build() (result *PublicIPAttachmentsServer, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}
	if b.attributionLogic == nil {
		err = errors.New("attribution logic is mandatory")
		return
	}

	inMapper, err := NewGenericMapper[*publicv1.PublicIPAttachment, *privatev1.PublicIPAttachment]().
		SetLogger(b.logger).
		SetStrict(true).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.PublicIPAttachment, *publicv1.PublicIPAttachment]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	delegate, err := NewPrivatePublicIPAttachmentsServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		SetFilterDesc((*publicv1.PublicIPAttachment)(nil).ProtoReflect().Descriptor()).
		Build()
	if err != nil {
		return
	}

	result = &PublicIPAttachmentsServer{
		logger:    b.logger,
		delegate:  delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *PublicIPAttachmentsServer) List(ctx context.Context,
	request *publicv1.PublicIPAttachmentsListRequest) (*publicv1.PublicIPAttachmentsListResponse, error) {
	// Create private request with same parameters:
	privateRequest := &privatev1.PublicIPAttachmentsListRequest{}
	privateRequest.SetOffset(request.GetOffset())
	privateRequest.SetLimit(request.GetLimit())
	privateRequest.SetFilter(request.GetFilter())
	privateRequest.SetOrder(request.GetOrder())

	privateResponse, err := s.delegate.List(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	privateItems := privateResponse.GetItems()
	publicItems := make([]*publicv1.PublicIPAttachment, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem, err := s.attachmentFromPrivate(ctx, privateItem)
		if err != nil {
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP attachment")
		}
		publicItems[i] = publicItem
	}

	response := &publicv1.PublicIPAttachmentsListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return response, nil
}

func (s *PublicIPAttachmentsServer) Get(ctx context.Context,
	request *publicv1.PublicIPAttachmentsGetRequest) (*publicv1.PublicIPAttachmentsGetResponse, error) {
	// Create private request:
	privateRequest := &privatev1.PublicIPAttachmentsGetRequest{}
	privateRequest.SetId(request.GetId())

	privateResponse, err := s.delegate.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	privateObject := privateResponse.GetObject()
	publicObject, err := s.attachmentFromPrivate(ctx, privateObject)
	if err != nil {
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP attachment")
	}

	response := &publicv1.PublicIPAttachmentsGetResponse{}
	response.SetObject(publicObject)
	return response, nil
}

func (s *PublicIPAttachmentsServer) Create(ctx context.Context,
	request *publicv1.PublicIPAttachmentsCreateRequest) (*publicv1.PublicIPAttachmentsCreateResponse, error) {
	// Map the public IP attachment to private format:
	publicObject := request.GetObject()
	if publicObject == nil {
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
	}
	privateObject, err := s.attachmentToPrivate(ctx, publicObject)
	if err != nil {
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP attachment")
	}

	privateRequest := &privatev1.PublicIPAttachmentsCreateRequest{}
	privateRequest.SetObject(privateObject)
	privateResponse, err := s.delegate.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	createdPrivateObject := privateResponse.GetObject()
	createdPublicObject, err := s.attachmentFromPrivate(ctx, createdPrivateObject)
	if err != nil {
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP attachment")
	}

	response := &publicv1.PublicIPAttachmentsCreateResponse{}
	response.SetObject(createdPublicObject)
	return response, nil
}

func (s *PublicIPAttachmentsServer) Update(ctx context.Context,
	request *publicv1.PublicIPAttachmentsUpdateRequest) (*publicv1.PublicIPAttachmentsUpdateResponse, error) {
	publicObject := request.GetObject()
	if publicObject == nil {
		return nil, grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
	}
	privateObject, err := s.attachmentToPrivate(ctx, publicObject)
	if err != nil {
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP attachment")
	}

	privateRequest := &privatev1.PublicIPAttachmentsUpdateRequest{}
	privateRequest.SetObject(privateObject)
	privateRequest.SetUpdateMask(request.GetUpdateMask())
	privateRequest.SetLock(request.GetLock())
	privateResponse, err := s.delegate.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	updatedPrivateObject := privateResponse.GetObject()
	updatedPublicObject, err := s.attachmentFromPrivate(ctx, updatedPrivateObject)
	if err != nil {
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process public IP attachment")
	}

	response := &publicv1.PublicIPAttachmentsUpdateResponse{}
	response.SetObject(updatedPublicObject)
	return response, nil
}

func (s *PublicIPAttachmentsServer) Delete(ctx context.Context,
	request *publicv1.PublicIPAttachmentsDeleteRequest) (*publicv1.PublicIPAttachmentsDeleteResponse, error) {
	// Create private request:
	privateRequest := &privatev1.PublicIPAttachmentsDeleteRequest{}
	privateRequest.SetId(request.GetId())

	_, err := s.delegate.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	response := &publicv1.PublicIPAttachmentsDeleteResponse{}
	return response, nil
}

func (s *PublicIPAttachmentsServer) attachmentFromPrivate(ctx context.Context, privateObj *privatev1.PublicIPAttachment) (*publicv1.PublicIPAttachment, error) {
	publicObj := &publicv1.PublicIPAttachment{}
	err := s.outMapper.Copy(ctx, privateObj, publicObj)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private public IP attachment to public",
			slog.Any("error", err),
		)
		return nil, err
	}
	return publicObj, nil
}

func (s *PublicIPAttachmentsServer) attachmentToPrivate(ctx context.Context, publicObj *publicv1.PublicIPAttachment) (*privatev1.PublicIPAttachment, error) {
	privateObj := &privatev1.PublicIPAttachment{}
	err := s.inMapper.Copy(ctx, publicObj, privateObj)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public IP attachment to private",
			slog.Any("error", err),
		)
		return nil, err
	}
	return privateObj, nil
}
