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
	"maps"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/events"
	"github.com/osac-project/fulfillment-service/internal/utils"
)

const bareMetalInstanceUserDataMaxBytes = 64 * 1024

type PrivateBareMetalInstancesServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.BareMetalInstancesServer = (*PrivateBareMetalInstancesServer)(nil)

type PrivateBareMetalInstancesServer struct {
	privatev1.UnimplementedBareMetalInstancesServer
	logger          *slog.Logger
	generic         *GenericServer[*privatev1.BareMetalInstance]
	catalogItemsDao *dao.GenericDAO[*privatev1.BareMetalInstanceCatalogItem]
	templatesDao    *dao.GenericDAO[*privatev1.BareMetalInstanceTemplate]
}

func NewPrivateBareMetalInstancesServer() *PrivateBareMetalInstancesServerBuilder {
	return &PrivateBareMetalInstancesServerBuilder{}
}

func (b *PrivateBareMetalInstancesServerBuilder) SetLogger(value *slog.Logger) *PrivateBareMetalInstancesServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateBareMetalInstancesServerBuilder) SetNotifier(value events.Notifier) *PrivateBareMetalInstancesServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateBareMetalInstancesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateBareMetalInstancesServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateBareMetalInstancesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateBareMetalInstancesServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateBareMetalInstancesServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateBareMetalInstancesServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateBareMetalInstancesServerBuilder) Build() (result *PrivateBareMetalInstancesServer, err error) {
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

	catalogItemsDao, err := dao.NewGenericDAO[*privatev1.BareMetalInstanceCatalogItem]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return result, err
	}

	templatesDao, err := dao.NewGenericDAO[*privatev1.BareMetalInstanceTemplate]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return result, err
	}

	generic, err := NewGenericServer[*privatev1.BareMetalInstance]().
		SetLogger(b.logger).
		SetService(privatev1.BareMetalInstances_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return result, err
	}

	result = &PrivateBareMetalInstancesServer{
		logger:          b.logger,
		generic:         generic,
		catalogItemsDao: catalogItemsDao,
		templatesDao:    templatesDao,
	}
	return result, err
}

func (s *PrivateBareMetalInstancesServer) List(ctx context.Context,
	request *privatev1.BareMetalInstancesListRequest) (response *privatev1.BareMetalInstancesListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstancesServer) Get(ctx context.Context,
	request *privatev1.BareMetalInstancesGetRequest) (response *privatev1.BareMetalInstancesGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstancesServer) Create(ctx context.Context,
	request *privatev1.BareMetalInstancesCreateRequest) (response *privatev1.BareMetalInstancesCreateResponse, err error) {
	if err = s.validateAndApplyCatalogItem(ctx, request.GetObject()); err != nil {
		return
	}
	if err = s.validateSpec(request.GetObject()); err != nil {
		return
	}
	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstancesServer) Update(ctx context.Context,
	request *privatev1.BareMetalInstancesUpdateRequest) (response *privatev1.BareMetalInstancesUpdateResponse, err error) {
	if err = s.validateImmutability(ctx, request); err != nil {
		return
	}
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstancesServer) Delete(ctx context.Context,
	request *privatev1.BareMetalInstancesDeleteRequest) (response *privatev1.BareMetalInstancesDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateBareMetalInstancesServer) Signal(ctx context.Context,
	request *privatev1.BareMetalInstancesSignalRequest) (response *privatev1.BareMetalInstancesSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// validateSpec validates fields on the bare metal instance spec that are checked at create time.
func (s *PrivateBareMetalInstancesServer) validateSpec(bmi *privatev1.BareMetalInstance) error {
	if bmi == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "bare metal instance is mandatory")
	}
	spec := bmi.GetSpec()
	if spec == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "bare metal instance spec is mandatory")
	}

	if spec.HasSshPublicKey() {
		sshPublicKey := spec.GetSshPublicKey()
		if sshPublicKey != "" {
			if err := validateOpenSSHPublicKey(sshPublicKey); err != nil {
				return grpcstatus.Errorf(grpccodes.InvalidArgument, "spec.ssh_public_key: %s", err.Error())
			}
		}
	}

	if spec.HasUserData() {
		userData := spec.GetUserData()
		if len(userData) > bareMetalInstanceUserDataMaxBytes {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"spec.user_data: size %d exceeds the maximum of %d bytes",
				len(userData), bareMetalInstanceUserDataMaxBytes)
		}
	}

	if spec.HasImage() {
		if err := s.validateBareMetalInstanceImage(spec.GetImage()); err != nil {
			return err
		}
	}

	return nil
}

// validateAndApplyCatalogItem verifies the referenced catalog item exists, is accessible,
// and applies its field definitions to the spec.
func (s *PrivateBareMetalInstancesServer) validateAndApplyCatalogItem(ctx context.Context,
	bmi *privatev1.BareMetalInstance) error {
	if bmi == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "bare metal instance is mandatory")
	}
	ref := bmi.GetSpec().GetCatalogItem()
	if ref == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "spec.catalog_item is mandatory")
	}

	response, err := s.catalogItemsDao.Get().
		SetId(ref).
		Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			return grpcstatus.Errorf(grpccodes.NotFound,
				"catalog item '%s' not found", ref)
		}
		s.logger.ErrorContext(ctx, "Failed to lookup bare metal instance catalog item",
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to lookup catalog item")
	}
	item := response.GetObject()

	if err := validateCatalogItemAccess(item, ref); err != nil {
		return err
	}

	if err := applyFieldDefinitions(bmi.GetSpec(), item.GetFieldDefinitions()); err != nil {
		return err
	}

	return s.validateAndApplyTemplateParameters(ctx, bmi, item.GetTemplate())
}

// validateAndApplyTemplateParameters fetches the template referenced by the catalog item,
// validates user-provided template_parameters against the template's parameter definitions,
// and applies default values for optional parameters.
func (s *PrivateBareMetalInstancesServer) validateAndApplyTemplateParameters(ctx context.Context,
	bmi *privatev1.BareMetalInstance, templateID string) error {
	providedParams := bmi.GetSpec().GetTemplateParameters()
	if templateID == "" {
		if len(providedParams) > 0 {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"spec.template_parameters can't be set because the catalog item has no template")
		}
		return nil
	}

	getResponse, err := s.templatesDao.Get().SetId(templateID).Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			if len(providedParams) == 0 {
				return nil
			}
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"template '%s' does not exist, cannot validate template_parameters", templateID)
		}
		s.logger.ErrorContext(ctx, "Failed to fetch template for parameter validation",
			slog.String("template_id", templateID),
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to fetch template")
	}
	template := getResponse.GetObject()

	s.applyBareMetalInstanceSpecDefaults(bmi.GetSpec(), template.GetSpecDefaults())

	if len(template.GetParameters()) == 0 && len(providedParams) == 0 {
		return nil
	}

	if err := utils.ValidateBareMetalInstanceTemplateParameters(template, providedParams); err != nil {
		return err
	}

	actualParams := utils.ProcessTemplateParametersWithDefaults(
		utils.BareMetalInstanceTemplateAdapter{BareMetalInstanceTemplate: template},
		providedParams,
	)
	bmi.GetSpec().SetTemplateParameters(actualParams)

	return nil
}

// validateImmutability ensures catalog_item, ssh_public_key, user_data, template_parameters,
// image, and auto_external_ip_attachment cannot be changed after creation.
func (s *PrivateBareMetalInstancesServer) validateImmutability(ctx context.Context,
	request *privatev1.BareMetalInstancesUpdateRequest) error {
	mask := request.GetUpdateMask()
	updatingCatalogItem := updateIncludesField(mask, "spec.catalog_item")
	updatingSshKey := updateIncludesField(mask, "spec.ssh_public_key")
	updatingUserData := updateIncludesField(mask, "spec.user_data")
	updatingTemplateParams := updateIncludesField(mask, "spec.template_parameters")
	updatingImage := updateIncludesField(mask, "spec.image")
	updatingAutoExternalIP := updateIncludesField(mask, "spec.auto_external_ip_attachment")

	bmi := request.GetObject()
	if bmi == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "bare metal instance is mandatory")
	}
	newSpec := bmi.GetSpec()
	if newSpec == nil && (updatingCatalogItem || updatingSshKey || updatingUserData || updatingTemplateParams || updatingImage || updatingAutoExternalIP) {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "bare metal instance spec is mandatory")
	}
	id := bmi.GetId()
	if id == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "bare metal instance id is mandatory")
	}

	getResponse, err := s.generic.dao.Get().SetId(id).Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			return grpcstatus.Errorf(grpccodes.NotFound, "bare metal instance '%s' not found", id)
		}
		s.logger.ErrorContext(ctx, "Failed to fetch bare metal instance for immutability check",
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to fetch bare metal instance")
	}
	existing := getResponse.GetObject()
	existingSpec := existing.GetSpec()
	if existingSpec == nil {
		return grpcstatus.Errorf(grpccodes.Internal, "stored bare metal instance is missing spec")
	}

	if updatingCatalogItem && existingSpec.GetCatalogItem() != newSpec.GetCatalogItem() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"cannot change spec.catalog_item from '%s' to '%s': catalog_item is immutable",
			existingSpec.GetCatalogItem(), newSpec.GetCatalogItem())
	}

	if updatingSshKey && existingSpec.GetSshPublicKey() != newSpec.GetSshPublicKey() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"cannot change spec.ssh_public_key: ssh_public_key is immutable after creation")
	}

	if updatingUserData && existingSpec.GetUserData() != newSpec.GetUserData() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"cannot change spec.user_data: user_data is immutable after creation")
	}

	if updatingTemplateParams {
		templateParamsEqual := func(first, second *anypb.Any) bool {
			return proto.Equal(first, second)
		}
		if !maps.EqualFunc(existingSpec.GetTemplateParameters(), newSpec.GetTemplateParameters(), templateParamsEqual) {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"cannot change spec.template_parameters: template parameters are immutable")
		}
	}

	if updatingImage && !proto.Equal(existingSpec.GetImage(), newSpec.GetImage()) {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"cannot change spec.image: image is immutable after creation")
	}

	if updatingAutoExternalIP && existingSpec.GetAutoExternalIpAttachment() != newSpec.GetAutoExternalIpAttachment() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"cannot change spec.auto_external_ip_attachment: auto_external_ip_attachment is immutable after creation")
	}

	return nil
}

func (s *PrivateBareMetalInstancesServer) applyBareMetalInstanceSpecDefaults(spec *privatev1.BareMetalInstanceSpec, defaults *privatev1.BareMetalInstanceTemplateSpecDefaults) {
	if spec == nil || defaults == nil {
		return
	}
	if !defaults.HasImage() {
		return
	}
	if !spec.HasImage() {
		spec.SetImage(proto.Clone(defaults.GetImage()).(*privatev1.BareMetalInstanceImage))
		return
	}
	img := spec.GetImage()
	defImg := defaults.GetImage()
	if img.GetSourceType() == "" && defImg.GetSourceType() != "" {
		img.SetSourceType(defImg.GetSourceType())
	}
	if img.GetSourceRef() == "" && defImg.GetSourceRef() != "" {
		img.SetSourceRef(defImg.GetSourceRef())
	}
}

func (s *PrivateBareMetalInstancesServer) validateBareMetalInstanceImage(image *privatev1.BareMetalInstanceImage) error {
	if image == nil {
		return nil
	}
	var missing []string
	if image.GetSourceType() == "" {
		missing = append(missing, "image.source_type")
	}
	if image.GetSourceRef() == "" {
		missing = append(missing, "image.source_ref")
	}
	if len(missing) > 0 {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"the following required image fields are missing: %s",
			strings.Join(missing, ", "),
		)
	}
	return nil
}
