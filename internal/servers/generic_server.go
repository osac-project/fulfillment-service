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
	"reflect"
	"strings"
	"sync"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/masks"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

// GenericServerBuilder contains the data and logic needed to create new generic servers.
type GenericServerBuilder[O dao.Object] struct {
	logger           *slog.Logger
	service          string
	table            string
	ignoredFields    []any
	notifier         *database.Notifier
	attributionLogic auth.AttributionLogic
	tenancyLogic     auth.TenancyLogic
}

// GenericServer is a gRPC server that knows how to implement the List, Get, Create, Update and Delete operators for
// any object that has identifier and metadata fields.
type GenericServer[O dao.Object] struct {
	logger         *slog.Logger
	service        string
	dao            *dao.GenericDAO[O]
	template       proto.Message
	metadataField  protoreflect.FieldDescriptor
	nameField      protoreflect.FieldDescriptor
	listRequest    proto.Message
	listResponse   proto.Message
	getRequest     proto.Message
	getResponse    proto.Message
	createRequest  proto.Message
	createResponse proto.Message
	updateRequest  proto.Message
	updateResponse proto.Message
	deleteRequest  proto.Message
	deleteResponse proto.Message
	signalRequest  proto.Message
	signalResponse proto.Message
	notifier       *database.Notifier
	pathCompiler   *masks.PathCompiler[O]
	pathCache      map[string]*masks.Path[O]
	pathCacheLock  *sync.Mutex
}

type metadataIface interface {
	proto.Message
	GetName() string
	GetLabels() map[string]string
	GetAnnotations() map[string]string
}

// NewGenericServer creates a builder that can then be used to configure and create a new generic server.
func NewGenericServer[O dao.Object]() *GenericServerBuilder[O] {
	return &GenericServerBuilder[O]{}
}

// SetLogger sets the logger. This is mandatory.
func (b *GenericServerBuilder[O]) SetLogger(value *slog.Logger) *GenericServerBuilder[O] {
	b.logger = value
	return b
}

// SetService sets the service description. This is mandatory.
func (b *GenericServerBuilder[O]) SetService(value string) *GenericServerBuilder[O] {
	b.service = value
	return b
}

// SetTable sets the name of the table where the objects will be stored. This is mandatory.
func (b *GenericServerBuilder[O]) SetTable(value string) *GenericServerBuilder[O] {
	b.table = value
	return b
}

// AddIgnoredFields adds a set of fields to be omitted when mapping objects. The values passed can be of the following
// types:
//
// string - This should be a field name, for example 'status' and then any field with that name in any object will
// be ignored.
//
// protoreflect.Name - Like string.
//
// protoreflect.FullName - This indicates a field of a particular type. For example, if the value is
// 'fulfillment.v1.Cluster.status' then the field 'status' of the 'fulfillment.v1.Cluster' type will be ignored, but
// the 'status' field of other types will not be ignored.
func (b *GenericServerBuilder[O]) AddIgnoredFields(values ...any) *GenericServerBuilder[O] {
	b.ignoredFields = append(b.ignoredFields, values...)
	return b
}

// SetNotifier sets the notifier that the server will use to send change notifications. This is optional.
func (b *GenericServerBuilder[O]) SetNotifier(value *database.Notifier) *GenericServerBuilder[O] {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the logic that will be used to determine the creators for objects.
func (b *GenericServerBuilder[O]) SetAttributionLogic(value auth.AttributionLogic) *GenericServerBuilder[O] {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic that will be used to determine the tenants for objects. The logic receives the
// context as a parameter and should return the names of the tenants. If not provided, no tenants will be set.
func (b *GenericServerBuilder[O]) SetTenancyLogic(value auth.TenancyLogic) *GenericServerBuilder[O] {
	b.tenancyLogic = value
	return b
}

// Build uses the configuration stored in the builder to create and configure a new generic server.
func (b *GenericServerBuilder[O]) Build() (result *GenericServer[O], err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.service == "" {
		err = errors.New("service name is mandatory")
		return
	}
	if b.table == "" {
		err = errors.New("table name is mandatory")
		return
	}

	// Create the path compiler:
	pathCompiler, err := masks.NewPathCompiler[O]().
		SetLogger(b.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create path compiler: %w", err)
		return
	}

	// Create the object early so that we can use its methods as callbacks:
	s := &GenericServer[O]{
		logger:        b.logger,
		service:       b.service,
		notifier:      b.notifier,
		pathCompiler:  pathCompiler,
		pathCache:     map[string]*masks.Path[O]{},
		pathCacheLock: &sync.Mutex{},
	}

	// Create the DAO:
	daoBuilder := dao.NewGenericDAO[O]()
	daoBuilder.SetLogger(b.logger)
	daoBuilder.SetTable(b.table)
	if b.notifier != nil {
		daoBuilder.AddEventCallback(s.notifyEvent)
	}
	if b.attributionLogic != nil {
		daoBuilder.SetAttributionLogic(b.attributionLogic)
	}
	if b.tenancyLogic != nil {
		daoBuilder.SetTenancyLogic(b.tenancyLogic)
	}
	s.dao, err = daoBuilder.Build()
	if err != nil {
		err = fmt.Errorf("failed to create DAO: %w", err)
		return
	}

	// Find the descriptor:
	service, err := b.findService()
	if err != nil {
		return
	}

	// Prepare the template for the object:
	var object O
	reflect := object.ProtoReflect()
	s.template = reflect.New().Interface()

	// Find the metadata field:
	descriptor := reflect.Descriptor()
	fields := descriptor.Fields()
	s.metadataField = fields.ByName("metadata")
	if s.metadataField == nil {
		err = fmt.Errorf("object of type '%s' doesn't have a 'metadata' field", descriptor.FullName())
		return
	}

	// Prepare templates for the request and response types. These are empty messages that will be cloned when
	// it is necessary to create new instances.
	s.listRequest, s.listResponse, err = b.findRequestAndResponse(service, listMethod)
	if err != nil {
		return
	}
	s.getRequest, s.getResponse, err = b.findRequestAndResponse(service, getMethod)
	if err != nil {
		return
	}
	s.createRequest, s.createResponse, err = b.findRequestAndResponse(service, createMethod)
	if err != nil {
		return
	}
	s.deleteRequest, s.deleteResponse, err = b.findRequestAndResponse(service, deleteMethod)
	if err != nil {
		return
	}
	s.updateRequest, s.updateResponse, err = b.findRequestAndResponse(service, updateMethod)
	if err != nil {
		return
	}
	s.signalRequest, s.signalResponse, err = b.findRequestAndResponse(service, signalMethod)
	if err != nil {
		return
	}

	result = s
	return
}

// findService finds the service descriptor using the service name given to the builder.
func (b *GenericServerBuilder[O]) findService() (result protoreflect.ServiceDescriptor, err error) {
	protoregistry.GlobalFiles.RangeFilesByPackage("private.v1", func(desc protoreflect.FileDescriptor) bool {
		for i := range desc.Services().Len() {
			serviceDesc := desc.Services().Get(i)
			if string(serviceDesc.FullName()) == b.service {
				result = serviceDesc
				return false
			}
		}
		return true
	})
	if result == nil {
		err = fmt.Errorf("failed to find service '%s'", b.service)
		return
	}
	return
}

// Names of gRPC methods:
const (
	listMethod   = "List"
	getMethod    = "Get"
	createMethod = "Create"
	updateMethod = "Update"
	deleteMethod = "Delete"
	signalMethod = "Signal"
)

// findRequestAndResponse finds the request and response message types for the given method.
func (b *GenericServerBuilder[O]) findRequestAndResponse(service protoreflect.ServiceDescriptor,
	methodName string) (request proto.Message, response proto.Message, err error) {
	for i := range service.Methods().Len() {
		method := service.Methods().Get(i)
		if string(method.Name()) == methodName {
			requestType, err := protoregistry.GlobalTypes.FindMessageByName(method.Input().FullName())
			if err != nil {
				return nil, nil, fmt.Errorf(
					"failed to find request message type '%s': %w",
					method.Input().FullName(), err,
				)
			}
			responseType, err := protoregistry.GlobalTypes.FindMessageByName(method.Output().FullName())
			if err != nil {
				return nil, nil, fmt.Errorf(
					"failed to find response message type '%s': %w",
					method.Output().FullName(), err,
				)
			}
			request = requestType.New().Interface()
			response = responseType.New().Interface()
			return request, response, nil
		}
	}
	err = fmt.Errorf("failed to find method '%s' in service '%s'", methodName, service.FullName())
	return
}

func (s *GenericServer[O]) List(ctx context.Context, request any, response any) error {
	type requestIface interface {
		GetOffset() int32
		GetLimit() int32
		GetFilter() string
	}
	type responseIface interface {
		SetSize(int32)
		SetTotal(int32)
		SetItems([]O)
	}
	requestMsg := request.(requestIface)
	daoResponse, err := s.dao.List().
		SetFilter(requestMsg.GetFilter()).
		SetOffset(requestMsg.GetOffset()).
		SetLimit(requestMsg.GetLimit()).
		Do(ctx)
	if err != nil {
		var deniedErr *dao.ErrDenied
		if errors.As(err, &deniedErr) {
			return grpcstatus.Errorf(grpccodes.PermissionDenied, "%s", deniedErr.Reason)
		}
		s.logger.ErrorContext(
			ctx,
			"Failed to list",
			slog.Any("error", err),
		)
		return grpcstatus.Errorf(grpccodes.Internal, "failed to list")
	}
	responseMsg := proto.Clone(s.listResponse).(responseIface)
	responseMsg.SetSize(daoResponse.GetSize())
	responseMsg.SetTotal(daoResponse.GetTotal())
	responseMsg.SetItems(daoResponse.GetItems())
	s.setPointer(response, responseMsg)
	return nil
}

func (s *GenericServer[O]) Get(ctx context.Context, request any, response any) error {
	type requestIface interface {
		GetId() string
	}
	type responseIface interface {
		SetObject(O)
	}
	requestMsg := request.(requestIface)
	id := requestMsg.GetId()
	if id == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "identifier is mandatory")
	}
	daoResponse, err := s.dao.Get().
		SetId(id).
		Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			return grpcstatus.Errorf(grpccodes.NotFound, "object with identifier '%s' not found", id)
		}
		var deniedErr *dao.ErrDenied
		if errors.As(err, &deniedErr) {
			return grpcstatus.Errorf(grpccodes.PermissionDenied, "%s", deniedErr.Reason)
		}
		s.logger.ErrorContext(
			ctx,
			"Failed to get",
			slog.String("id", id),
			slog.Any("error", err),
		)
		return grpcstatus.Errorf(grpccodes.Internal, "failed to get object with identifier '%s'", id)
	}
	object := daoResponse.GetObject()
	responseMsg := proto.Clone(s.getResponse).(responseIface)
	responseMsg.SetObject(object)
	s.setPointer(response, responseMsg)
	return nil
}

func (s *GenericServer[O]) Create(ctx context.Context, request any, response any) error {
	type requestIface interface {
		GetObject() O
	}
	type responseIface interface {
		SetObject(O)
	}
	requestMsg := request.(requestIface)
	object := requestMsg.GetObject()
	if !s.isNil(object) {
		metadata := s.getMetadata(object)
		if metadata != nil {
			err := s.validateMetadata(metadata)
			if err != nil {
				return err
			}
		}
	}
	daoRequest := s.dao.Create()
	if !s.isNil(object) {
		daoRequest.SetObject(object)
	}
	daoResponse, err := daoRequest.Do(ctx)
	if err != nil {
		var alreadyExistsErr *dao.ErrAlreadyExists
		if errors.As(err, &alreadyExistsErr) {
			return grpcstatus.Errorf(
				grpccodes.AlreadyExists,
				"object with identifier '%s' already exists",
				alreadyExistsErr.ID,
			)
		}
		var deniedErr *dao.ErrDenied
		if errors.As(err, &deniedErr) {
			return grpcstatus.Errorf(grpccodes.PermissionDenied, "%s", deniedErr.Reason)
		}
		s.logger.ErrorContext(
			ctx,
			"Failed to create",
			slog.Any("error", err),
		)
		return grpcstatus.Errorf(grpccodes.Internal, "failed to create object")
	}
	object = daoResponse.GetObject()
	responseMsg := proto.Clone(s.createResponse).(responseIface)
	responseMsg.SetObject(object)
	s.setPointer(response, responseMsg)
	return nil
}

func (s *GenericServer[O]) Update(ctx context.Context, request any, response any) error {
	type requestIface interface {
		GetObject() O
		GetUpdateMask() *fieldmaskpb.FieldMask
	}
	type responseIface interface {
		SetObject(O)
	}
	requestMsg := request.(requestIface)
	input := requestMsg.GetObject()
	if s.isNil(input) {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
	}
	id := input.GetId()
	if id == "" {
		return grpcstatus.Errorf(grpccodes.Internal, "object identifier is mandatory")
	}

	// Fetch the current representation of the object:
	getResponse, err := s.dao.Get().
		SetId(id).
		Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			return grpcstatus.Errorf(
				grpccodes.NotFound,
				"object with identifier '%s' not found",
				id,
			)
		}
		var deniedErr *dao.ErrDenied
		if errors.As(err, &deniedErr) {
			return grpcstatus.Errorf(grpccodes.PermissionDenied, "%s", deniedErr.Reason)
		}
		s.logger.ErrorContext(
			ctx,
			"Failed to get object",
			slog.String("id", id),
			slog.Any("error", err),
		)
		return grpcstatus.Errorf(
			grpccodes.Internal,
			"failed to get object with identifier '%s'",
			id,
		)
	}
	object := getResponse.GetObject()
	if s.isNil(object) {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"object with identifier '%s' doesn't exist",
			id,
		)
	}

	// Update the fields indicated in the mask, or all the fields if there is no mask:
	mask := requestMsg.GetUpdateMask()
	if mask != nil {
		fieldPaths, err := s.compilePaths(mask.GetPaths())
		if err != nil {
			return err
		}
		for _, fieldPath := range fieldPaths {
			value, ok := fieldPath.Get(input)
			if ok {
				fieldPath.Set(object, value)
			} else {
				fieldPath.Clear(object)
			}
		}
	} else {
		object = input
	}

	// Validate the metadata:
	metadata := s.getMetadata(object)
	if metadata != nil {
		err = s.validateMetadata(metadata)
		if err != nil {
			return err
		}
	}

	// Save the result:
	updateResponse, err := s.dao.Update().
		SetObject(object).
		Do(ctx)
	if err != nil {
		var deniedErr *dao.ErrDenied
		if errors.As(err, &deniedErr) {
			return grpcstatus.Errorf(grpccodes.PermissionDenied, "%s", deniedErr.Reason)
		}
		s.logger.ErrorContext(
			ctx,
			"Failed to update object",
			slog.String("id", id),
			slog.Any("error", err),
		)
		return grpcstatus.Errorf(
			grpccodes.Internal,
			"failed to update object with identifier '%s'",
			id,
		)
	}
	object = updateResponse.GetObject()

	responseMsg := proto.Clone(s.updateResponse).(responseIface)
	responseMsg.SetObject(object)
	s.setPointer(response, responseMsg)
	return nil
}

func (s *GenericServer[O]) compilePaths(paths []string) (result []*masks.Path[O], err error) {
	fieldPaths := make([]*masks.Path[O], len(paths))
	for i, path := range paths {
		fieldPaths[i], err = s.compilePath(path)
		if err != nil {
			return
		}
	}
	result = fieldPaths
	return
}

func (s *GenericServer[O]) compilePath(path string) (result *masks.Path[O], err error) {
	s.pathCacheLock.Lock()
	defer s.pathCacheLock.Unlock()
	result, ok := s.pathCache[path]
	if ok {
		return
	}
	result, err = s.pathCompiler.Compile(path)
	if err != nil {
		return
	}
	s.pathCache[path] = result
	return
}

func (s *GenericServer[O]) Delete(ctx context.Context, request any, response any) error {
	type requestIface interface {
		GetId() string
	}
	type responseIface interface {
	}
	requestMsg := request.(requestIface)
	id := requestMsg.GetId()
	if id == "" {
		return grpcstatus.Errorf(grpccodes.Internal, "object identifier is mandatory")
	}
	_, err := s.dao.Delete().
		SetId(id).
		Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			return grpcstatus.Errorf(
				grpccodes.NotFound,
				"object with identifier '%s' not found",
				id,
			)
		}
		var deniedErr *dao.ErrDenied
		if errors.As(err, &deniedErr) {
			return grpcstatus.Errorf(grpccodes.PermissionDenied, "%s", deniedErr.Reason)
		}
		s.logger.ErrorContext(
			ctx,
			"Failed to delete object",
			slog.String("id", id),
			slog.Any("error", err),
		)
		return grpcstatus.Errorf(
			grpccodes.Internal,
			"failed to delete object with identifier '%s'",
			id,
		)
	}
	responseMsg := proto.Clone(s.deleteResponse).(responseIface)
	s.setPointer(response, responseMsg)
	return nil
}

func (s *GenericServer[O]) Signal(ctx context.Context, request any, response any) error {
	type requestIface interface {
		GetId() string
	}
	requestMsg := request.(requestIface)
	id := requestMsg.GetId()
	if id == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "identifier is mandatory")
	}
	daoResponse, err := s.dao.Get().
		SetId(id).
		Do(ctx)
	if err != nil {
		var notFoundErr *dao.ErrNotFound
		if errors.As(err, &notFoundErr) {
			return grpcstatus.Errorf(
				grpccodes.NotFound,
				"object with identifier '%s' not found",
				id,
			)
		}
		var deniedErr *dao.ErrDenied
		if errors.As(err, &deniedErr) {
			return grpcstatus.Errorf(grpccodes.PermissionDenied, "%s", deniedErr.Reason)
		}
		s.logger.ErrorContext(
			ctx,
			"Failed to signal object",
			slog.String("id", id),
			slog.Any("error", err),
		)
		return grpcstatus.Errorf(
			grpccodes.Internal,
			"failed to signal object with identifier '%s'",
			id,
		)
	}
	object := daoResponse.GetObject()
	event := privatev1.Event_builder{
		Id:   uuid.New(),
		Type: privatev1.EventType_EVENT_TYPE_OBJECT_SIGNALED,
	}.Build()
	err = s.setPayload(event, object)
	if err != nil {
		return err
	}
	if s.notifier != nil {
		err = s.notifier.Notify(ctx, event)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to send signal notification",
				slog.String("id", id),
				slog.Any("error", err),
			)
		}
	}
	responseMsg := proto.Clone(s.signalResponse)
	s.setPointer(response, responseMsg)
	return nil
}

// notifyEvent converts the DAO event into an API event and publishes it using the PostgreSQL NOTIFY command.
func (s *GenericServer[O]) notifyEvent(ctx context.Context, e dao.Event) error {
	// TODO: This is the only part of the generic server that depends on specific object types. Is there a way
	// to avoid that?
	event := &privatev1.Event{}
	event.SetId(uuid.New())
	switch e.Type {
	case dao.EventTypeCreated:
		event.SetType(privatev1.EventType_EVENT_TYPE_OBJECT_CREATED)
	case dao.EventTypeUpdated:
		event.SetType(privatev1.EventType_EVENT_TYPE_OBJECT_UPDATED)
	case dao.EventTypeDeleted:
		event.SetType(privatev1.EventType_EVENT_TYPE_OBJECT_DELETED)
	default:
		return fmt.Errorf("unknown event kind '%s'", e.Type)
	}
	err := s.setPayload(event, e.Object)
	if err != nil {
		return err
	}
	return s.notifier.Notify(ctx, event)
}

func (s *GenericServer[O]) setPayload(event *privatev1.Event, object proto.Message) error {
	// TODO: This is the only part of the generic server that depends on specific object types. Is there a way
	// to avoid that?
	switch object := object.(type) {
	case *privatev1.ClusterTemplate:
		event.SetClusterTemplate(object)
	case *privatev1.Cluster:
		event.SetCluster(object)
	case *privatev1.HostClass:
		event.SetHostClass(object)
	case *privatev1.Host:
		event.SetHost(object)
	case *privatev1.HostPool:
		event.SetHostPool(object)
	case *privatev1.Hub:
		// TODO: We need to remove the Kubeconfig from the payload of the notification because that usually
		// exceeds the default limit of 8000 bytes of the PostgreSQL notification mechanism. A better way to
		// do this would be to store the payloads in a separate table. We will do that later.
		object = proto.Clone(object).(*privatev1.Hub)
		object.SetKubeconfig(nil)
		event.SetHub(object)
	case *privatev1.ComputeInstanceTemplate:
		event.SetComputeInstanceTemplate(object)
	case *privatev1.ComputeInstance:
		event.SetComputeInstance(object)
	default:
		return fmt.Errorf("unknown object type '%T'", object)
	}
	return nil
}

func (s *GenericServer[O]) isNil(object proto.Message) bool {
	return reflect.ValueOf(object).IsNil()
}

func (s *GenericServer[O]) setPointer(pointer any, value any) {
	reflect.ValueOf(pointer).Elem().Set(reflect.ValueOf(value))
}

func (s *GenericServer[O]) validateMetadata(metadata metadataIface) error {
	name := metadata.GetName()
	if name != "" {
		err := s.validateName(name)
		if err != nil {
			return err
		}
	}
	labels := metadata.GetLabels()
	if len(labels) > 0 {
		err := s.validateLabels(labels)
		if err != nil {
			return err
		}
	}
	annotations := metadata.GetAnnotations()
	if len(annotations) > 0 {
		err := s.validateAnnotations(annotations)
		if err != nil {
			return err
		}
	}
	return nil
}

// validateName validates that the 'metadata.name' field follows DNS label restrictions as defined in RFC 1035:
//
// - Must be between 1 and 63 characters long
// - Must only contain lowercase letters (a-z), digits (0-9) and hyphens (-)
// - Cannot start or end with a hyphen
func (s *GenericServer[O]) validateName(name string) error {
	// Max length:
	if len(name) > 63 {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field 'metadata.name' must be at most 63 characters long, but it has %d characters",
			len(name),
		)
	}

	// Validate characters, only a-z, 0-9, and hyphen:
	for i, c := range name {
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		isHyphen := c == '-'
		if !isLower && !isDigit && !isHyphen {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"field 'metadata.name' must only contain lowercase letters (a-z), digits (0-9) and "+
					"hyphens (-), but contains '%c' at position %d",
				c, i,
			)
		}
	}

	// Cannot start or end with hyphen:
	if strings.HasPrefix(name, "-") {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field 'metadata.name' cannot start with a hyphen",
		)
	}
	if strings.HasSuffix(name, "-") {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field 'metadata.name' cannot end with a hyphen",
		)
	}

	return nil
}

func (s *GenericServer[O]) validateLabels(labels map[string]string) error {
	for key, value := range labels {
		err := s.validateLabelKey("metadata.labels", key)
		if err != nil {
			return err
		}
		if value == "" {
			continue
		}
		err = s.validateLabelNameOrValue("metadata.labels", key, "value", value)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *GenericServer[O]) validateAnnotations(annotations map[string]string) error {
	for key := range annotations {
		err := s.validateLabelKey("metadata.annotations", key)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *GenericServer[O]) validateLabelKey(field string, key string) error {
	if key == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field '%s' has empty key", field)
	}
	parts := strings.Split(key, "/")
	if len(parts) > 2 {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field '%s' key '%s' must contain at most one '/'",
			field, key,
		)
	}
	var prefix string
	var name string
	if len(parts) == 2 {
		prefix = parts[0]
		name = parts[1]
		if prefix == "" || name == "" {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"field '%s' key '%s' must have non-empty prefix and name",
				field, key,
			)
		}
	} else {
		name = parts[0]
	}
	if prefix != "" {
		err := s.validateLabelPrefix(field, key, prefix)
		if err != nil {
			return err
		}
	}
	return s.validateLabelNameOrValue(field, key, "name", name)
}

func (s *GenericServer[O]) validateLabelPrefix(field string, key string, prefix string) error {
	if len(prefix) < 1 || len(prefix) > 253 {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field '%s' key '%s' prefix must be between 1 and 253 characters long",
			field, key,
		)
	}
	segments := strings.Split(prefix, ".")
	for _, segment := range segments {
		if segment == "" {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"field '%s' key '%s' prefix must not contain empty segments",
				field, key,
			)
		}
		err := s.validateDNSLabel(field, key, "prefix segment", segment)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *GenericServer[O]) validateDNSLabel(field string, key string, labelKind string, label string) error {
	if len(label) > 63 {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field '%s' key '%s' %s must be at most 63 characters long",
			field, key, labelKind,
		)
	}
	for i, c := range label {
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		isHyphen := c == '-'
		if !isLower && !isDigit && !isHyphen {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"field '%s' key '%s' %s must only contain lowercase letters (a-z), digits (0-9) and "+
					"hyphens (-), but contains '%c' at position %d",
				field, key, labelKind, c, i,
			)
		}
	}
	if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field '%s' key '%s' %s cannot start or end with a hyphen",
			field, key, labelKind,
		)
	}
	return nil
}

func (s *GenericServer[O]) validateLabelNameOrValue(field string, key string, labelKind string, value string) error {
	if len(value) < 1 || len(value) > 63 {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field '%s' key '%s' %s must be between 1 and 63 characters long",
			field, key, labelKind,
		)
	}
	for i, c := range value {
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		isHyphen := c == '-'
		isUnderscore := c == '_'
		isDot := c == '.'
		if !isLower && !isDigit && !isHyphen && !isUnderscore && !isDot {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"field '%s' key '%s' %s must only contain lowercase letters (a-z), digits (0-9), "+
					"hyphens (-), underscores (_) or dots (.), but contains '%c' at position %d",
				field, key, labelKind, c, i,
			)
		}
	}
	first := value[0]
	last := value[len(value)-1]
	firstIsAlnum := (first >= 'a' && first <= 'z') || (first >= '0' && first <= '9')
	lastIsAlnum := (last >= 'a' && last <= 'z') || (last >= '0' && last <= '9')
	if !firstIsAlnum || !lastIsAlnum {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field '%s' key '%s' %s must start and end with an alphanumeric character",
			field, key, labelKind,
		)
	}
	return nil
}

func (s *GenericServer[O]) getMetadata(object O) metadataIface {
	objectReflect := object.ProtoReflect()
	if !objectReflect.Has(s.metadataField) {
		return nil
	}
	return objectReflect.Get(s.metadataField).Message().Interface().(metadataIface)
}
