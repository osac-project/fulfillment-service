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
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/events"
)

type PrivatePublicIPAttachmentsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
	filterDesc        protoreflect.MessageDescriptor
}

var _ privatev1.PublicIPAttachmentsServer = (*PrivatePublicIPAttachmentsServer)(nil)

type PrivatePublicIPAttachmentsServer struct {
	privatev1.UnimplementedPublicIPAttachmentsServer

	logger                *slog.Logger
	generic               *GenericServer[*privatev1.PublicIPAttachment]
	publicIPDao           *dao.GenericDAO[*privatev1.PublicIP]
	computeInstanceDao    *dao.GenericDAO[*privatev1.ComputeInstance]
	publicIPAttachmentDao *dao.GenericDAO[*privatev1.PublicIPAttachment]
}

func NewPrivatePublicIPAttachmentsServer() *PrivatePublicIPAttachmentsServerBuilder {
	return &PrivatePublicIPAttachmentsServerBuilder{}
}

func (b *PrivatePublicIPAttachmentsServerBuilder) SetLogger(value *slog.Logger) *PrivatePublicIPAttachmentsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivatePublicIPAttachmentsServerBuilder) SetNotifier(value events.Notifier) *PrivatePublicIPAttachmentsServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivatePublicIPAttachmentsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivatePublicIPAttachmentsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivatePublicIPAttachmentsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivatePublicIPAttachmentsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivatePublicIPAttachmentsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivatePublicIPAttachmentsServerBuilder {
	b.metricsRegisterer = value
	return b
}

// SetFilterDesc sets the protobuf message descriptor used to validate and translate CEL filter expressions. This is
// optional. When unset, the private object type is used. Public servers that wrap this private server should pass the
// corresponding public object descriptor so that clients cannot filter on private-only fields.
func (b *PrivatePublicIPAttachmentsServerBuilder) SetFilterDesc(value protoreflect.MessageDescriptor) *PrivatePublicIPAttachmentsServerBuilder {
	b.filterDesc = value
	return b
}

func (b *PrivatePublicIPAttachmentsServerBuilder) Build() (*PrivatePublicIPAttachmentsServer, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	if b.tenancyLogic == nil {
		return nil, errors.New("tenancy logic is mandatory")
	}

	publicIPDao, err := dao.NewGenericDAO[*privatev1.PublicIP]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return nil, err
	}

	computeInstanceDao, err := dao.NewGenericDAO[*privatev1.ComputeInstance]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return nil, err
	}

	publicIPAttachmentDao, err := dao.NewGenericDAO[*privatev1.PublicIPAttachment]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return nil, err
	}

	generic, err := NewGenericServer[*privatev1.PublicIPAttachment]().
		SetLogger(b.logger).
		SetService(privatev1.PublicIPAttachments_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		SetFilterDesc(b.filterDesc).
		Build()
	if err != nil {
		return nil, err
	}

	result := &PrivatePublicIPAttachmentsServer{
		logger:                b.logger,
		generic:               generic,
		publicIPDao:           publicIPDao,
		computeInstanceDao:    computeInstanceDao,
		publicIPAttachmentDao: publicIPAttachmentDao,
	}
	return result, nil
}

func (s *PrivatePublicIPAttachmentsServer) List(ctx context.Context,
	request *privatev1.PublicIPAttachmentsListRequest) (response *privatev1.PublicIPAttachmentsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivatePublicIPAttachmentsServer) Get(ctx context.Context,
	request *privatev1.PublicIPAttachmentsGetRequest) (response *privatev1.PublicIPAttachmentsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

// Create validates references, enforces uniqueness, creates the attachment, and sets
// PublicIP.status.attached. All operations share the gRPC interceptor's database
// transaction: if any step fails, the entire transaction rolls back.
func (s *PrivatePublicIPAttachmentsServer) Create(ctx context.Context,
	request *privatev1.PublicIPAttachmentsCreateRequest) (response *privatev1.PublicIPAttachmentsCreateResponse, err error) {
	attachment := request.GetObject()

	err = s.validatePublicIPAttachment(attachment)
	if err != nil {
		return
	}

	spec := attachment.GetSpec()
	publicIPID := spec.GetPublicIp()
	computeInstanceID := spec.GetComputeInstance()

	err = s.validatePublicIPReference(ctx, publicIPID)
	if err != nil {
		return
	}

	err = s.validateComputeInstanceReference(ctx, computeInstanceID)
	if err != nil {
		return
	}

	err = s.validateUniqueness(ctx, publicIPID, computeInstanceID)
	if err != nil {
		return
	}

	if attachment.GetStatus() == nil {
		attachment.SetStatus(privatev1.PublicIPAttachmentStatus_builder{
			State: privatev1.PublicIPAttachmentState_PUBLIC_IP_ATTACHMENT_STATE_PENDING,
		}.Build())
	} else {
		attachment.GetStatus().SetState(
			privatev1.PublicIPAttachmentState_PUBLIC_IP_ATTACHMENT_STATE_PENDING)
	}

	err = s.generic.Create(ctx, request, &response)
	if err != nil {
		return
	}

	err = s.updatePublicIPAttachedFlag(ctx, publicIPID, true)
	if err != nil {
		return
	}

	return
}

func (s *PrivatePublicIPAttachmentsServer) Update(ctx context.Context,
	request *privatev1.PublicIPAttachmentsUpdateRequest) (response *privatev1.PublicIPAttachmentsUpdateResponse, err error) {
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	mask := request.GetUpdateMask()
	if updateIncludesField(mask, "spec.public_ip", "spec.compute_instance") {
		getRequest := &privatev1.PublicIPAttachmentsGetRequest{}
		getRequest.SetId(id)
		var getResponse *privatev1.PublicIPAttachmentsGetResponse
		err = s.generic.Get(ctx, getRequest, &getResponse)
		if err != nil {
			return
		}
		err = validateImmutableFieldsPublicIPAttachment(request.GetObject(), getResponse.GetObject())
		if err != nil {
			return
		}
	}

	err = s.generic.Update(ctx, request, &response)
	return
}

// Delete removes the attachment and clears PublicIP.status.attached.
// All operations share the gRPC interceptor's transaction for atomicity.
func (s *PrivatePublicIPAttachmentsServer) Delete(ctx context.Context,
	request *privatev1.PublicIPAttachmentsDeleteRequest) (response *privatev1.PublicIPAttachmentsDeleteResponse, err error) {
	id := request.GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	getRequest := &privatev1.PublicIPAttachmentsGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.PublicIPAttachmentsGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	publicIPID := getResponse.GetObject().GetSpec().GetPublicIp()

	err = s.generic.Delete(ctx, request, &response)
	if err != nil {
		return
	}

	if publicIPID != "" {
		err = s.updatePublicIPAttachedFlag(ctx, publicIPID, false)
		if err != nil {
			return
		}
	}

	return
}

func (s *PrivatePublicIPAttachmentsServer) Signal(ctx context.Context,
	request *privatev1.PublicIPAttachmentsSignalRequest) (response *privatev1.PublicIPAttachmentsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

func (s *PrivatePublicIPAttachmentsServer) validatePublicIPAttachment(
	attachment *privatev1.PublicIPAttachment) error {
	if attachment == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "public IP attachment is mandatory")
	}
	spec := attachment.GetSpec()
	if spec == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "public IP attachment spec is mandatory")
	}
	if spec.GetPublicIp() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.public_ip' is required")
	}
	if !spec.HasComputeInstance() || spec.GetComputeInstance() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.compute_instance' is required")
	}
	return nil
}

func validateImmutableFieldsPublicIPAttachment(
	newAttachment, existingAttachment *privatev1.PublicIPAttachment) error {
	newSpec := newAttachment.GetSpec()
	existingSpec := existingAttachment.GetSpec()

	if newPublicIP := newSpec.GetPublicIp(); newPublicIP != existingSpec.GetPublicIp() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.public_ip' is immutable and cannot be changed from '%s' to '%s'",
			existingSpec.GetPublicIp(), newPublicIP)
	}

	if newCI := newSpec.GetComputeInstance(); newCI != existingSpec.GetComputeInstance() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'spec.compute_instance' is immutable and cannot be changed from '%s' to '%s'",
			existingSpec.GetComputeInstance(), newCI)
	}

	return nil
}

func (s *PrivatePublicIPAttachmentsServer) validatePublicIPReference(
	ctx context.Context, publicIPID string) error {
	getResponse, err := s.publicIPDao.Get().
		SetId(publicIPID).
		Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"PublicIP '%s' does not exist", publicIPID)
		}
		s.logger.ErrorContext(ctx, "Failed to query PublicIP",
			slog.String("public_ip_id", publicIPID),
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to validate public_ip")
	}

	publicIP := getResponse.GetObject()

	if publicIP.GetStatus().GetState() != privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED {
		return grpcstatus.Errorf(grpccodes.FailedPrecondition,
			"PublicIP '%s' is not in ALLOCATED state (current state: %s)",
			publicIPID, publicIP.GetStatus().GetState().String())
	}

	return nil
}

func (s *PrivatePublicIPAttachmentsServer) validateComputeInstanceReference(
	ctx context.Context, computeInstanceID string) error {
	getResponse, err := s.computeInstanceDao.Get().
		SetId(computeInstanceID).
		Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"ComputeInstance '%s' does not exist", computeInstanceID)
		}
		s.logger.ErrorContext(ctx, "Failed to query ComputeInstance",
			slog.String("compute_instance_id", computeInstanceID),
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to validate compute_instance")
	}

	ci := getResponse.GetObject()
	if ci.GetStatus().GetState() != privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING {
		return grpcstatus.Errorf(grpccodes.FailedPrecondition,
			"ComputeInstance '%s' is not in RUNNING state (current state: %s)",
			computeInstanceID, ci.GetStatus().GetState().String())
	}

	return nil
}

func (s *PrivatePublicIPAttachmentsServer) validateUniqueness(
	ctx context.Context, publicIPID string, computeInstanceID string) error {
	pipFilter := "this.spec.public_ip == " + strconv.Quote(publicIPID)
	pipResp, err := s.publicIPAttachmentDao.List().
		SetFilter(pipFilter).
		SetLimit(1).
		Do(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to list attachments for uniqueness check",
			slog.String("public_ip_id", publicIPID),
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to validate uniqueness")
	}
	if pipResp.GetSize() > 0 {
		return grpcstatus.Errorf(grpccodes.AlreadyExists,
			"a PublicIPAttachment already exists for PublicIP '%s'", publicIPID)
	}

	ciFilter := "this.spec.compute_instance == " + strconv.Quote(computeInstanceID)
	ciResp, err := s.publicIPAttachmentDao.List().
		SetFilter(ciFilter).
		SetLimit(1).
		Do(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to list attachments for uniqueness check",
			slog.String("compute_instance_id", computeInstanceID),
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to validate uniqueness")
	}
	if ciResp.GetSize() > 0 {
		return grpcstatus.Errorf(grpccodes.AlreadyExists,
			"a PublicIPAttachment already exists for ComputeInstance '%s'", computeInstanceID)
	}

	return nil
}

func (s *PrivatePublicIPAttachmentsServer) updatePublicIPAttachedFlag(
	ctx context.Context, publicIPID string, attached bool) error {
	getResponse, err := s.publicIPDao.Get().
		SetId(publicIPID).
		Do(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to get PublicIP for attached flag update",
			slog.String("public_ip_id", publicIPID),
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to update PublicIP attached status")
	}

	publicIP := getResponse.GetObject()
	publicIP.GetStatus().SetAttached(attached)

	_, err = s.publicIPDao.Update().
		SetObject(publicIP).
		Do(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to update PublicIP attached flag",
			slog.String("public_ip_id", publicIPID),
			slog.Bool("attached", attached),
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to update PublicIP attached status")
	}

	return nil
}
