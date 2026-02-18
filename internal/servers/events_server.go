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
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	eventsv1 "github.com/osac-project/fulfillment-service/internal/api/events/v1"
	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

type EventsServerBuilder struct {
	logger       *slog.Logger
	flags        *pflag.FlagSet
	dbUrl        string
	tenancyLogic auth.TenancyLogic
}

var _ eventsv1.EventsServer = (*EventsServer)(nil)

type EventsServer struct {
	eventsv1.UnimplementedEventsServer

	logger       *slog.Logger
	listener     *database.Listener
	subs         map[string]eventsServerSubInfo
	subsLock     *sync.RWMutex
	celEnv       *cel.Env
	mapper       *GenericMapper[*privatev1.Event, *eventsv1.Event]
	tenancyLogic auth.TenancyLogic
}

type eventsServerSubInfo struct {
	stream     grpc.ServerStreamingServer[eventsv1.EventsWatchResponse]
	subject    *auth.Subject
	filterSrc  string
	filterPrg  cel.Program
	eventsChan chan *eventsv1.Event
}

func NewEventsServer() *EventsServerBuilder {
	return &EventsServerBuilder{}
}

func (b *EventsServerBuilder) SetLogger(value *slog.Logger) *EventsServerBuilder {
	b.logger = value
	return b
}

func (b *EventsServerBuilder) SetFlags(value *pflag.FlagSet) *EventsServerBuilder {
	b.flags = value
	return b
}

func (b *EventsServerBuilder) SetDbUrl(value string) *EventsServerBuilder {
	b.dbUrl = value
	return b
}

func (b *EventsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *EventsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *EventsServerBuilder) Build() (result *EventsServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.dbUrl == "" {
		err = errors.New("database connection URL is mandatory")
		return
	}

	// Create  the CEL environment:
	celEnv, err := b.createCelEnv()
	if err != nil {
		err = fmt.Errorf("failed to create CEL environment: %w", err)
		return
	}

	// Create the mappers:
	mapper, err := NewGenericMapper[*privatev1.Event, *eventsv1.Event]().
		SetLogger(b.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create mapper: %w", err)
		return
	}

	// Create the tenancy logic:
	tenancyLogic := b.tenancyLogic
	if tenancyLogic == nil {
		tenancyLogic, err = auth.NewGuestTenancyLogic().
			SetLogger(b.logger).
			Build()
		if err != nil {
			err = fmt.Errorf("failed to create tenancy logic: %w", err)
			return
		}
	}

	// Create the object early so that whe can use its methods as callback functions:
	s := &EventsServer{
		logger:       b.logger,
		subs:         map[string]eventsServerSubInfo{},
		subsLock:     &sync.RWMutex{},
		celEnv:       celEnv,
		mapper:       mapper,
		tenancyLogic: tenancyLogic,
	}

	// Create the notification listener:
	s.listener, err = database.NewListener().
		SetLogger(b.logger).
		SetUrl(b.dbUrl).
		SetChannel("events").
		AddPayloadCallback(s.processPayload).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create notification listener: %w", err)
		return
	}

	result = s
	return
}

func (b *EventsServerBuilder) createCelEnv() (result *cel.Env, err error) {
	// Declare contants for the enum types of the package:
	var options []cel.EnvOption
	protoregistry.GlobalTypes.RangeEnums(func(enumType protoreflect.EnumType) bool {
		enumDesc := enumType.Descriptor()
		if !eventsServerPackages[enumDesc.FullName().Parent()] {
			return true
		}
		enumValues := enumDesc.Values()
		for i := range enumValues.Len() {
			valueDesc := enumValues.Get(i)
			valueName := string(valueDesc.Name())
			valueNumber := valueDesc.Number()
			valueConst := cel.Constant(valueName, cel.IntType, types.Int(valueNumber))
			options = append(options, valueConst)
			b.logger.Debug(
				"Added enum constant",
				slog.String("type", string(enumDesc.FullName())),
				slog.String("name", valueName),
				slog.Int64("value", int64(valueNumber)),
			)
		}
		return true
	})

	// Declare the event type:
	var eventModel *eventsv1.Event
	options = append(options, cel.Types(eventModel))

	// Declare the event variable:
	eventDesc := eventModel.ProtoReflect().Descriptor()
	eventType := cel.ObjectType(string(eventDesc.FullName()))
	options = append(options, cel.Variable("event", eventType))

	// Create the CEL environment:
	result, err = cel.NewEnv(options...)
	return
}

// Starts starts the background components of the server, in particular the notification listener. This is a blocking
// operation, and will return only when the context is canceled.
func (s *EventsServer) Start(ctx context.Context) error {
	return s.listener.Listen(ctx)
}

func (s *EventsServer) Watch(request *eventsv1.EventsWatchRequest,
	stream grpc.ServerStreamingServer[eventsv1.EventsWatchResponse]) (err error) {
	// Get the context:
	ctx := stream.Context()

	// Get the subject:
	subject := auth.SubjectFromContext(ctx)

	// Compile the filter expression:
	var (
		filterSrc string
		filterPrg cel.Program
	)
	if request.Filter != nil {
		filterSrc = *request.Filter
		if filterSrc != "" {
			filterPrg, err = s.compileFilter(ctx, filterSrc)
			if err != nil {
				s.logger.ErrorContext(
					ctx,
					"Failed to compile filter",
					slog.String("filter", filterSrc),
					slog.Any("error", err),
				)
				return grpcstatus.Errorf(
					grpccodes.InvalidArgument,
					"failed to compile filter '%s'",
					filterSrc,
				)
			}
		}
	}

	// Create a subscription and remember to remove it when done:
	subId := uuid.New()
	logger := s.logger.With(
		slog.String("subscription", subId),
	)
	subInfo := eventsServerSubInfo{
		stream:     stream,
		subject:    subject,
		filterSrc:  filterSrc,
		filterPrg:  filterPrg,
		eventsChan: make(chan *eventsv1.Event),
	}
	s.subsLock.Lock()
	s.subs[subId] = subInfo
	s.subsLock.Unlock()
	logger.DebugContext(ctx, "Created subcription")
	defer func() {
		s.subsLock.Lock()
		defer s.subsLock.Unlock()
		delete(s.subs, subId)
		close(subInfo.eventsChan)
		logger.DebugContext(ctx, "Canceled subcription")
	}()

	// Wait to receive events on the channel of the subscription and forward them to the client:
	for {
		select {
		case event, ok := <-subInfo.eventsChan:
			if !ok {
				logger.DebugContext(ctx, "Subscription channel closed")
				return nil
			}
			err = stream.Send(eventsv1.EventsWatchResponse_builder{
				Event: event,
			}.Build())
			if err != nil {
				return err
			}
		case <-stream.Context().Done():
			s.logger.DebugContext(ctx, "Subscription context canceled")
			return nil
		}
	}
}

func (s *EventsServer) compileFilter(ctx context.Context, filterSrc string) (result cel.Program, err error) {
	tree, issues := s.celEnv.Compile(filterSrc)
	err = issues.Err()
	if err != nil {
		return
	}
	result, err = s.celEnv.Program(tree)
	return
}

func (s *EventsServer) evalFilter(ctx context.Context, filterPrg cel.Program, event *eventsv1.Event) (result bool,
	err error) {
	activation, err := cel.NewActivation(map[string]any{
		"event": event,
	})
	if err != nil {
		return
	}
	value, _, err := filterPrg.ContextEval(ctx, activation)
	if err != nil {
		return
	}
	result, ok := value.Value().(bool)
	if !ok {
		err = fmt.Errorf("result of filter should be a boolean, but it is of type '%T'", result)
		return
	}
	return
}

func (s *EventsServer) processPayload(ctx context.Context, payload proto.Message) error {
	// Get the object:
	private, ok := payload.(*privatev1.Event)
	if !ok {
		s.logger.ErrorContext(
			ctx,
			"Unexpected payload type",
			slog.String("expected", fmt.Sprintf("%T", private)),
			slog.String("actual", fmt.Sprintf("%T", payload)),
		)
		return nil
	}

	// Skip signal events:
	if private.GetType() == privatev1.EventType_EVENT_TYPE_OBJECT_SIGNALED {
		return nil
	}

	// Skip object that don't have a public representtion:
	if private.HasHub() {
		return nil
	}

	// Translate the private event to a public event and process it:
	public := &eventsv1.Event{}
	err := s.mapper.Copy(ctx, private, public)
	if err != nil {
		return fmt.Errorf("failed to translate event: %w", err)
	}
	return s.processEvent(ctx, public, private)
}

// checkTenancy checks if the object is visible to the current user.
func (s *EventsServer) checkTenancy(ctx context.Context, event *privatev1.Event) (result bool, err error) {
	// Get the visible tenants for the current user:
	visibleTenants, err := s.tenancyLogic.DetermineVisibleTenants(ctx)
	if err != nil {
		err = fmt.Errorf("failed to determine visible tenants: %w", err)
		return
	}
	if visibleTenants.Empty() {
		result = true
		return
	}

	// Get the tenants of the object:
	objectTenants := s.extractTenants(ctx, event)

	// Calculate the intersection of the visible tenants and the object tenants:
	commonTenants := visibleTenants.Intersection(objectTenants)

	// If the intersection is empty, thn the user can see the event, otherwise they can't.
	if commonTenants.Empty() {
		s.logger.DebugContext(
			ctx,
			"Event is not visible to the current user",
			slog.Any("event", event),
			slog.Any("visibible_tenants", visibleTenants),
			slog.Any("object_tenants", objectTenants),
		)
		result = false
		return
	}
	s.logger.DebugContext(
		ctx,
		"Event is visible to the current user",
		slog.Any("event", event),
		slog.Any("visibible_tenants", visibleTenants),
		slog.Any("object_tenants", objectTenants),
	)
	result = true
	return
}

func (s *EventsServer) extractTenants(ctx context.Context, event *privatev1.Event) collections.Set[string] {
	metadata := s.extractMetadata(ctx, event)
	return collections.NewSet(metadata.GetTenants()...)
}

func (s *EventsServer) extractMetadata(ctx context.Context, event *privatev1.Event) *privatev1.Metadata {
	switch {
	case event.HasCluster():
		return event.GetCluster().GetMetadata()
	case event.HasClusterTemplate():
		return event.GetClusterTemplate().GetMetadata()
	case event.HasHostClass():
		return event.GetHostClass().GetMetadata()
	case event.HasHost():
		return event.GetHost().GetMetadata()
	case event.HasHostPool():
		return event.GetHostPool().GetMetadata()
	case event.HasComputeInstanceTemplate():
		return event.GetComputeInstanceTemplate().GetMetadata()
	case event.HasComputeInstance():
		return event.GetComputeInstance().GetMetadata()
	default:
		s.logger.ErrorContext(
			ctx,
			"Unexpected event type",
			slog.Any("event", event),
		)
		return nil
	}
}

func (s *EventsServer) processEvent(ctx context.Context, public *eventsv1.Event, private *privatev1.Event) error {
	s.subsLock.RLock()
	defer s.subsLock.RUnlock()
	for subId, sub := range s.subs {
		logger := s.logger.With(
			slog.String("filter", sub.filterSrc),
			slog.String("sub", subId),
			slog.Any("public", public),
			slog.Any("private", private),
		)
		accepted := true

		// In order to check the tenancy, and maybe for other things as well, we need to have a context that
		// looks like the context passed to a service method. In particular we need to have the subject of the
		// user. So we need to create a new context.
		ctx := auth.ContextWithSubject(ctx, sub.subject)

		// Check if the user has permission to see the event:
		visible, err := s.checkTenancy(ctx, private)
		if err != nil {
			return fmt.Errorf("failed to check tenancy: %w", err)
		}
		if !visible {
			return nil
		}

		// Apply user-defined filter:
		if sub.filterPrg != nil {
			var err error
			accepted, err = s.evalFilter(ctx, sub.filterPrg, public)
			if err != nil {
				logger.DebugContext(
					ctx,
					"Failed to evaluate filter",
					slog.Any("error", err),
				)
				accepted = false
			}
		}

		// Forward the event to the subscription:
		if accepted {
			logger.DebugContext(ctx, "Event accepted by filter")
			sub.eventsChan <- public
		} else {
			logger.DebugContext(ctx, "Event rejected by filter")
		}
	}
	return nil
}

// Names of the packages whose enums will be available in the filter expressions:
var eventsServerPackages = map[protoreflect.FullName]bool{
	"events.v1":      true,
	"fulfillment.v1": true,
}
