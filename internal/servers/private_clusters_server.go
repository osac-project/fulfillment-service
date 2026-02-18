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
	"sort"
	"strconv"
	"strings"

	"github.com/bits-and-blooms/bitset"
	"github.com/dustin/go-humanize/english"
	"golang.org/x/exp/maps"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/utils"
)

type PrivateClustersServerBuilder struct {
	logger           *slog.Logger
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

var _ privatev1.ClustersServer = (*PrivateClustersServer)(nil)

type PrivateClustersServer struct {
	privatev1.UnimplementedClustersServer
	logger         *slog.Logger
	templatesDao   *dao.GenericDAO[*privatev1.ClusterTemplate]
	hostClassesDao *dao.GenericDAO[*privatev1.HostClass]
	generic        *GenericServer[*privatev1.Cluster]
}

func NewPrivateClustersServer() *PrivateClustersServerBuilder {
	return &PrivateClustersServerBuilder{}
}

func (b *PrivateClustersServerBuilder) SetLogger(value *slog.Logger) *PrivateClustersServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateClustersServerBuilder) SetNotifier(value *database.Notifier) *PrivateClustersServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateClustersServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateClustersServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateClustersServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateClustersServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateClustersServerBuilder) Build() (result *PrivateClustersServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the templates DAO:
	templatesDao, err := dao.NewGenericDAO[*privatev1.ClusterTemplate]().
		SetLogger(b.logger).
		SetTable("cluster_templates").
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create the host classes DAO:
	hostClassesDao, err := dao.NewGenericDAO[*privatev1.HostClass]().
		SetLogger(b.logger).
		SetTable("host_classes").
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.Cluster]().
		SetLogger(b.logger).
		SetService(privatev1.Clusters_ServiceDesc.ServiceName).
		SetTable("clusters").
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateClustersServer{
		logger:         b.logger,
		templatesDao:   templatesDao,
		hostClassesDao: hostClassesDao,
		generic:        generic,
	}
	return
}

func (s *PrivateClustersServer) List(ctx context.Context,
	request *privatev1.ClustersListRequest) (response *privatev1.ClustersListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateClustersServer) Get(ctx context.Context,
	request *privatev1.ClustersGetRequest) (response *privatev1.ClustersGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateClustersServer) Create(ctx context.Context,
	request *privatev1.ClustersCreateRequest) (response *privatev1.ClustersCreateResponse, err error) {
	// Ensure sane defaults:
	s.setDefaults(request.GetObject())

	// Get the spec:
	spec := request.GetObject().GetSpec()

	// The user may have specified the host classes of the node sets by name, but we want to save the
	// identifiers, so we need to look them up:
	for _, nodeSet := range spec.GetNodeSets() {
		var hostClass *privatev1.HostClass
		hostClass, err = s.lookupHostClass(ctx, nodeSet.GetHostClass())
		if err != nil {
			return
		}
		nodeSet.SetHostClass(hostClass.GetId())
	}

	// Validate duplicate conditions first:
	err = s.validateNoDuplicateConditions(request.GetObject())
	if err != nil {
		return
	}

	// Validate template and perform transformations:
	err = s.validateAndTransformCluster(ctx, request.GetObject())
	if err != nil {
		return
	}

	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateClustersServer) Update(ctx context.Context,
	request *privatev1.ClustersUpdateRequest) (response *privatev1.ClustersUpdateResponse, err error) {
	err = s.validateNoDuplicateConditions(request.GetObject())
	if err != nil {
		return
	}
	err = s.validateTemplateImmutability(ctx, request)
	if err != nil {
		return
	}
	err = s.validateNodeSetsUpdate(ctx, request)
	if err != nil {
		return
	}
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateClustersServer) Delete(ctx context.Context,
	request *privatev1.ClustersDeleteRequest) (response *privatev1.ClustersDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateClustersServer) Signal(ctx context.Context,
	request *privatev1.ClustersSignalRequest) (response *privatev1.ClustersSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

func (s *PrivateClustersServer) setDefaults(cluster *privatev1.Cluster) {
	if !cluster.HasSpec() {
		cluster.SetSpec(&privatev1.ClusterSpec{})
	}
	if !cluster.HasStatus() {
		cluster.SetStatus(&privatev1.ClusterStatus{})
	}
}

func (s *PrivateClustersServer) lookupTemplate(ctx context.Context,
	key string) (result *privatev1.ClusterTemplate, err error) {
	if key == "" {
		return
	}
	response, err := s.templatesDao.List().
		SetFilter(fmt.Sprintf("this.id == %[1]s || this.metadata.name == %[1]s", strconv.Quote(key))).
		SetLimit(1).
		Do(ctx)
	if err != nil {
		var deniedErr *dao.ErrDenied
		if errors.As(err, &deniedErr) {
			err = grpcstatus.Errorf(grpccodes.PermissionDenied, "%s", deniedErr.Reason)
		}
		return
	}
	switch response.GetSize() {
	case 0:
		err = grpcstatus.Errorf(
			grpccodes.NotFound,
			"there is no template with identifier or name '%s'",
			key,
		)
	case 1:
		result = response.GetItems()[0]
	default:
		err = grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"there are multiple templates with identifier or name '%s'",
			key,
		)
	}
	return
}

func (s *PrivateClustersServer) lookupHostClass(ctx context.Context,
	key string) (result *privatev1.HostClass, err error) {
	if key == "" {
		return
	}
	response, err := s.hostClassesDao.List().
		SetFilter(fmt.Sprintf("this.id == %[1]s || this.metadata.name == %[1]s", strconv.Quote(key))).
		SetLimit(1).
		Do(ctx)
	if err != nil {
		var deniedErr *dao.ErrDenied
		if errors.As(err, &deniedErr) {
			err = grpcstatus.Errorf(grpccodes.PermissionDenied, "%s", deniedErr.Reason)
		}
		return
	}
	switch response.GetSize() {
	case 0:
		err = grpcstatus.Errorf(
			grpccodes.NotFound,
			"there is no host class with identifier or name '%s'",
			key,
		)
	case 1:
		result = response.GetItems()[0]
	default:
		err = grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"there are multiple host classes with identifier or name '%s'",
			key,
		)
	}
	return
}

func (s *PrivateClustersServer) validateNoDuplicateConditions(object *privatev1.Cluster) error {
	conditions := object.GetStatus().GetConditions()
	if conditions == nil {
		return nil
	}
	conditionTypes := &bitset.BitSet{}
	for _, condition := range conditions {
		conditionType := condition.GetType()
		if conditionTypes.Test(uint(conditionType)) {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"condition '%s' is duplicated",
				conditionType.String(),
			)
		}
		conditionTypes.Set(uint(conditionType))
	}
	return nil
}

// validateNodeSetsUpdate validates that changes to node_sets are allowed.
// It delegates to specific validators for different aspects of the validation.
func (s *PrivateClustersServer) validateNodeSetsUpdate(ctx context.Context,
	request *privatev1.ClustersUpdateRequest) error {
	// Check if the update affects node_sets at all:
	if !s.updateAffectsNodeSets(request.GetUpdateMask()) {
		// Update doesn't touch node_sets, no validation needed
		return nil
	}

	// Check if only size fields are being updated - these are always allowed
	if s.isUpdatingOnlySizes(request.GetUpdateMask()) {
		return nil
	}

	// Fetch the existing cluster from the database:
	existingCluster, found, err := s.getExistingCluster(ctx, request)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	// Get the node sets from both clusters:
	existingNodeSets := existingCluster.GetSpec().GetNodeSets()
	newNodeSets := request.GetObject().GetSpec().GetNodeSets()

	// Run specific validations:
	if err := s.validateAtLeastOneNodeSet(newNodeSets); err != nil {
		return err
	}
	if err := s.validateNodeSetHostClassImmutability(existingNodeSets, newNodeSets); err != nil {
		return err
	}

	return nil
}

// getExistingCluster fetches the existing cluster from the database.
// Returns the cluster, a boolean indicating if it was found, and any error that occurred.
func (s *PrivateClustersServer) getExistingCluster(ctx context.Context,
	request *privatev1.ClustersUpdateRequest) (*privatev1.Cluster, bool, error) {
	cluster := request.GetObject()
	if cluster == nil {
		return nil, false, nil
	}
	id := cluster.GetId()
	if id == "" {
		return nil, false, nil
	}
	getResponse, err := s.generic.dao.Get().
		SetId(id).
		Do(ctx)
	if err != nil {
		return nil, false, err
	}
	existingCluster := getResponse.GetObject()
	return existingCluster, true, nil
}

// updateAffectsNodeSets checks if the update mask indicates that node_sets are being modified.
func (s *PrivateClustersServer) updateAffectsNodeSets(updateMask *fieldmaskpb.FieldMask) bool {
	if updateMask == nil {
		// No mask means no updates to node sets
		return false
	}
	return s.isFieldInMask(updateMask, "spec.node_sets")
}

// isFieldInMask checks if a field path is in the update mask.
func (s *PrivateClustersServer) isFieldInMask(updateMask *fieldmaskpb.FieldMask, fieldPath string) bool {
	if updateMask == nil {
		return false
	}
	for _, path := range updateMask.GetPaths() {
		if path == fieldPath || strings.HasPrefix(path, fieldPath+".") {
			return true
		}
	}
	return false
}

// isUpdatingOnlySizes checks if the update mask is only modifying size fields of node sets.
func (s *PrivateClustersServer) isUpdatingOnlySizes(updateMask *fieldmaskpb.FieldMask) bool {
	for _, path := range updateMask.GetPaths() {
		if strings.HasPrefix(path, "spec.node_sets") {
			if !strings.HasSuffix(path, ".size") {
				return false
			}
		}
	}
	return true
}

// validateAtLeastOneNodeSet ensures that clusters always have at least one node set.
func (s *PrivateClustersServer) validateAtLeastOneNodeSet(nodeSets map[string]*privatev1.ClusterNodeSet) error {
	if len(nodeSets) == 0 {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"cannot remove the last node set: clusters must have at least one node set",
		)
	}
	return nil
}

// validateNodeSetHostClassImmutability ensures that the host_class field of existing node sets
// cannot be changed. This is an existing documented restriction in the API specification.
func (s *PrivateClustersServer) validateNodeSetHostClassImmutability(
	existingNodeSets map[string]*privatev1.ClusterNodeSet,
	newNodeSets map[string]*privatev1.ClusterNodeSet) error {
	for nodeSetName, existingNodeSet := range existingNodeSets {
		newNodeSet, exists := newNodeSets[nodeSetName]
		if !exists {
			// Node set is being removed, which is allowed (if at least one remains)
			continue
		}
		existingHostClass := existingNodeSet.GetHostClass()
		newHostClass := newNodeSet.GetHostClass()
		if existingHostClass != newHostClass {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"cannot change host_class for node set '%s' from '%s' to '%s': host_class is immutable",
				nodeSetName,
				existingHostClass,
				newHostClass,
			)
		}
	}
	return nil
}

// validateTemplateImmutability ensures that the template and template_parameters fields
// cannot be changed after cluster creation.
func (s *PrivateClustersServer) validateTemplateImmutability(ctx context.Context,
	request *privatev1.ClustersUpdateRequest) error {
	// Check if template or template_parameters are being updated:
	updateMask := request.GetUpdateMask()
	updatingTemplate := s.isFieldInMask(updateMask, "spec.template")
	updatingTemplateParams := s.isFieldInMask(updateMask, "spec.template_parameters")

	// If neither field is being updated, no validation needed:
	if !updatingTemplate && !updatingTemplateParams {
		return nil
	}

	// Fetch the existing cluster from the database:
	existingCluster, found, err := s.getExistingCluster(ctx, request)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	// Get the specs from both clusters:
	existingSpec := existingCluster.GetSpec()
	newSpec := request.GetObject().GetSpec()

	// Check if template has changed:
	if updatingTemplate && existingSpec.GetTemplate() != newSpec.GetTemplate() {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"cannot change spec.template from '%s' to '%s': template is immutable",
			existingSpec.GetTemplate(),
			newSpec.GetTemplate(),
		)
	}

	// Check if template_parameters have changed:
	if updatingTemplateParams {
		templateParamsEqual := func(first, second *anypb.Any) bool {
			return proto.Equal(first, second)
		}
		if !maps.EqualFunc(existingSpec.GetTemplateParameters(), newSpec.GetTemplateParameters(), templateParamsEqual) {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"cannot change spec.template_parameters: template parameters are immutable",
			)
		}
	}

	return nil
}

func (s *PrivateClustersServer) validateAndTransformCluster(ctx context.Context, cluster *privatev1.Cluster) error {
	// Check that the template is specified and that refers to a existing template. If the reference was a name
	// then we replace it with the identifier.
	if cluster == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
	}
	templateRef := cluster.GetSpec().GetTemplate()
	if templateRef == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "template is mandatory")
	}
	template, err := s.lookupTemplate(ctx, templateRef)
	if err != nil {
		return err
	}
	if template.GetMetadata().HasDeletionTimestamp() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "template '%s' has been deleted", templateRef)
	}

	// Check that the host classes given in the cluster and the template exist, and index them by identifier and
	// name, so tha it will be easier to look them up later..
	hostClasses := map[string]*privatev1.HostClass{}
	for _, nodeSet := range template.GetNodeSets() {
		hostClassRef := nodeSet.GetHostClass()
		if hostClassRef == "" {
			continue
		}
		hostClass, err := s.lookupHostClass(ctx, hostClassRef)
		if err != nil {
			return err
		}
		hostClassName := hostClass.GetMetadata().GetName()
		if hostClassName != "" {
			hostClasses[hostClassName] = hostClass
		}
		hostClassId := hostClass.GetId()
		hostClasses[hostClassId] = hostClass
	}
	for _, nodeSet := range template.GetNodeSets() {
		hostClassRef := nodeSet.GetHostClass()
		hostClass, err := s.lookupHostClass(ctx, hostClassRef)
		if err != nil {
			return err
		}
		hostClassName := hostClass.GetMetadata().GetName()
		if hostClassName != "" {
			hostClasses[hostClassName] = hostClass
		}
		hostClassId := hostClass.GetId()
		hostClasses[hostClassId] = hostClass
	}

	// Check that all the node sets given in the cluster correspond to node sets that exist in the template:
	templateNodeSets := template.GetNodeSets()
	clusterNodeSets := cluster.GetSpec().GetNodeSets()
	for clusterNodeSetKey := range clusterNodeSets {
		templateNodeSet := templateNodeSets[clusterNodeSetKey]
		if templateNodeSet == nil {
			templateNodeSetKeys := maps.Keys(templateNodeSets)
			sort.Strings(templateNodeSetKeys)
			for i, templateNodeSetKey := range templateNodeSetKeys {
				templateNodeSetKeys[i] = fmt.Sprintf("'%s'", templateNodeSetKey)
			}
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"node set '%s' doesn't exist, valid values for template '%s' are %s",
				clusterNodeSetKey, templateRef, english.WordSeries(templateNodeSetKeys, "and"),
			)
		}
	}

	// Check that all the node sets given in the cluster specify the same host class that is specified in the
	// template:
	for clusterNodeSetKey, clusterNodeSet := range clusterNodeSets {
		clusterHostClassRef := clusterNodeSet.GetHostClass()
		if clusterHostClassRef == "" {
			continue
		}
		templateNodeSet := templateNodeSets[clusterNodeSetKey]
		templateHostClassRef := templateNodeSet.GetHostClass()
		templateHostClass := hostClasses[templateHostClassRef]
		templateHostClassId := templateHostClass.GetId()
		templateHostClassName := templateHostClass.GetMetadata().GetName()
		if templateHostClassName != "" {
			if clusterHostClassRef != templateHostClassId && clusterHostClassRef != templateHostClassName {
				return grpcstatus.Errorf(
					grpccodes.InvalidArgument,
					"host class for node set '%s' should be empty, '%s' or '%s', like in template '%s', "+
						"but it is '%s'",
					clusterNodeSetKey,
					templateHostClassName,
					templateHostClassId,
					templateRef,
					clusterHostClassRef,
				)
			}
		} else {
			if clusterHostClassRef != templateHostClassId {
				return grpcstatus.Errorf(
					grpccodes.InvalidArgument,
					"host class for node set '%s' should be empty or '%s', like in template '%s', "+
						"but it is '%s'",
					clusterNodeSetKey,
					templateHostClassId,
					templateRef,
					clusterHostClassRef,
				)
			}
		}
	}

	// Check that all the node sets given in the cluster have a positive size:
	for clusterNodeSetKey, clusterNodeSet := range clusterNodeSets {
		clusterNodeSetSize := clusterNodeSet.GetSize()
		if clusterNodeSetSize <= 0 {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"size for node set '%s' should be greater than zero, but it is %d",
				clusterNodeSetKey, clusterNodeSetSize,
			)
		}
	}

	// Replace the node sets given in the cluster with those from the template, taking only the size from cluster:
	actualNodeSets := map[string]*privatev1.ClusterNodeSet{}
	for templateNodeSetKey, templateNodeSet := range templateNodeSets {
		var actualNodeSetSize int32
		clusterNodeSet := clusterNodeSets[templateNodeSetKey]
		if clusterNodeSet != nil {
			actualNodeSetSize = clusterNodeSet.GetSize()
		} else {
			actualNodeSetSize = templateNodeSet.GetSize()
		}
		actualNodeSets[templateNodeSetKey] = privatev1.ClusterNodeSet_builder{
			HostClass: templateNodeSet.GetHostClass(),
			Size:      actualNodeSetSize,
		}.Build()
	}
	cluster.GetSpec().SetNodeSets(actualNodeSets)

	// Validate template parameters:
	clusterParameters := cluster.GetSpec().GetTemplateParameters()
	err = utils.ValidateClusterTemplateParameters(template, clusterParameters)
	if err != nil {
		return err
	}

	// Set default values for template parameters:
	actualClusterParameters := utils.ProcessTemplateParametersWithDefaults(
		utils.ClusterTemplateAdapter{ClusterTemplate: template},
		clusterParameters,
	)
	cluster.GetSpec().SetTemplateParameters(actualClusterParameters)

	// Make sure that the templte and the host classes of the node sets are reference by their identifiers, as that
	// is what we want to save to the database.
	cluster.GetSpec().SetTemplate(template.GetId())
	for _, clusterNodeSet := range cluster.GetSpec().GetNodeSets() {
		hostClassRef := clusterNodeSet.GetHostClass()
		hostClass := hostClasses[hostClassRef]
		clusterNodeSet.SetHostClass(hostClass.GetId())
	}

	return nil
}
