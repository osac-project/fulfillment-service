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
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/crypto/ssh"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/events"
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

	catalogItemsDao, err := dao.NewGenericDAO[*privatev1.BareMetalInstanceCatalogItem]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
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
		return
	}

	result = &PrivateBareMetalInstancesServer{
		logger:          b.logger,
		generic:         generic,
		catalogItemsDao: catalogItemsDao,
	}
	return
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
	if err = s.validateSpec(request.GetObject()); err != nil {
		return
	}
	if err = s.validateAndApplyCatalogItem(ctx, request.GetObject()); err != nil {
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

	if spec.HasSshKey() {
		sshKey := spec.GetSshKey()
		if sshKey != "" {
			if err := validateOpenSSHPublicKey(sshKey); err != nil {
				return grpcstatus.Errorf(grpccodes.InvalidArgument, "spec.ssh_key: %s", err.Error())
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

	response, err := s.catalogItemsDao.List().
		SetFilter(fmt.Sprintf("this.id == %[1]s || this.metadata.name == %[1]s", strconv.Quote(ref))).
		SetLimit(1).
		Do(ctx)
	if err != nil {
		var deniedErr *dao.ErrDenied
		if errors.As(err, &deniedErr) {
			return grpcstatus.Errorf(grpccodes.PermissionDenied, "%s", deniedErr.Reason)
		}
		s.logger.ErrorContext(ctx, "Failed to lookup bare metal instance catalog item",
			slog.String("ref", ref),
			slog.Any("error", err))
		return grpcstatus.Errorf(grpccodes.Internal, "failed to lookup catalog item")
	}
	items := response.GetItems()
	if len(items) == 0 {
		return grpcstatus.Errorf(grpccodes.NotFound,
			"there is no catalog item with identifier or name '%s'", ref)
	}
	item := items[0]

	if err := validateCatalogItemAccess(item, ref); err != nil {
		return err
	}

	return applyFieldDefinitions(bmi.GetSpec(), item.GetFieldDefinitions())
}

// validateImmutability ensures catalog_item, ssh_key, and user_data cannot be changed after creation.
func (s *PrivateBareMetalInstancesServer) validateImmutability(ctx context.Context,
	request *privatev1.BareMetalInstancesUpdateRequest) error {
	mask := request.GetUpdateMask()
	fullReplace := mask == nil || len(mask.GetPaths()) == 0
	updatingCatalogItem := fullReplace || hasMaskPrefix(mask, "spec.catalog_item")
	updatingSshKey := fullReplace || hasMaskPrefix(mask, "spec.ssh_key")
	updatingUserData := fullReplace || hasMaskPrefix(mask, "spec.user_data")

	bmi := request.GetObject()
	if bmi == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "bare metal instance is mandatory")
	}
	newSpec := bmi.GetSpec()
	if newSpec == nil && (updatingCatalogItem || updatingSshKey || updatingUserData) {
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
			slog.String("id", id),
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

	if updatingSshKey && existingSpec.GetSshKey() != newSpec.GetSshKey() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"cannot change spec.ssh_key: ssh_key is immutable after creation")
	}

	if updatingUserData && existingSpec.GetUserData() != newSpec.GetUserData() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"cannot change spec.user_data: user_data is immutable after creation")
	}

	return nil
}

// validateOpenSSHPublicKey checks that the provided string is a valid OpenSSH public key.
func validateOpenSSHPublicKey(key string) error {
	_, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(key))
	if err != nil {
		return fmt.Errorf("invalid OpenSSH public key: %w", err)
	}
	if len(rest) > 0 {
		return fmt.Errorf("invalid OpenSSH public key: unexpected trailing content")
	}
	return nil
}
