/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package get

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
)

// watch watches for events and displays updated objects.
func (c *runnerContext) watch(ctx context.Context, keys []string) error {
	// Build filter for events
	filter, err := c.buildEventFilter(keys)
	if err != nil {
		return fmt.Errorf("failed to build event filter: %w", err)
	}

	// Create events client
	eventsClient := publicv1.NewEventsClient(c.conn)

	// Start watching
	c.console.Printf(ctx, "Watching for changes (Ctrl+C to stop)...\n\n")

	stream, err := eventsClient.Watch(ctx, &publicv1.EventsWatchRequest{
		Filter: &filter,
	})
	if err != nil {
		return fmt.Errorf("failed to start watching events: %w", err)
	}

	// Process events
	for {
		response, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to receive event: %w", err)
		}

		event := response.GetEvent()
		if event == nil {
			continue
		}

		// Extract the object from the event payload
		object, err := c.extractObjectFromEvent(event)
		if err != nil {
			c.logger.WarnContext(
				ctx,
				"Failed to extract object from event",
				"event_id", event.GetId(),
				"error", err,
			)
			continue
		}

		if object == nil {
			continue
		}

		// Display the event
		c.displayEvent(ctx, event, object)
	}
}

// buildEventFilter builds a CEL filter expression for watching events.
func (c *runnerContext) buildEventFilter(keys []string) (string, error) {
	// Get the field name for this object type in the Event message
	fieldName := c.getEventPayloadFieldName()
	if fieldName == "" {
		return "", fmt.Errorf("object type '%s' is not supported for watching", c.objectHelper)
	}

	// Build filter parts
	var parts []string

	// Filter by object type (check if the payload field is set)
	parts = append(parts, fmt.Sprintf("has(event.%s)", fieldName))

	// If specific IDs/names are provided, filter by them
	if len(keys) > 0 {
		var idFilters []string
		for _, key := range keys {
			// Match by ID or name
			idFilters = append(idFilters, fmt.Sprintf(
				"event.%s.id == %q || event.%s.metadata.name == %q",
				fieldName, key, fieldName, key,
			))
		}
		parts = append(parts, "("+strings.Join(idFilters, " || ")+")")
	}

	return strings.Join(parts, " && "), nil
}

// Map of proto message full names to event payload field names
var eventPayloadFieldNames = map[string]string{
	string(proto.MessageName((*publicv1.Cluster)(nil))):         "cluster",
	string(proto.MessageName((*publicv1.ClusterTemplate)(nil))): "cluster_template",
}

// getEventPayloadFieldName returns the field name in the Event message for the current object type.
func (c *runnerContext) getEventPayloadFieldName() string {
	fullName := string(c.objectHelper.Descriptor().FullName())
	return eventPayloadFieldNames[fullName]
}

// extractObjectFromEvent extracts the proto message from an event payload.
func (c *runnerContext) extractObjectFromEvent(event *publicv1.Event) (proto.Message, error) {
	payload := event.GetPayload()
	if payload == nil {
		return nil, fmt.Errorf("event has no payload")
	}

	// Use type switch on the oneof payload
	switch p := payload.(type) {
	case *publicv1.Event_Cluster:
		return p.Cluster, nil
	case *publicv1.Event_ClusterTemplate:
		return p.ClusterTemplate, nil
	default:
		return nil, fmt.Errorf("unsupported event payload type")
	}
}

// displayEvent displays an event and the updated object.
func (c *runnerContext) displayEvent(ctx context.Context, event *publicv1.Event, object proto.Message) {
	timestamp := time.Now().Format(time.TimeOnly)
	eventType := strings.TrimPrefix(event.GetType().String(), "EVENT_TYPE_")

	objectId := c.getObjectId(object)

	c.console.Printf(ctx, "[%s] %s %s '%s'\n", timestamp, eventType, c.objectHelper.Singular(), objectId)

	var render func(context.Context, []proto.Message) error
	switch c.args.format {
	case outputFormatJson:
		render = c.renderJson
	case outputFormatYaml:
		render = c.renderYaml
	default:
		render = c.renderTable
	}

	err := render(ctx, []proto.Message{object})
	if err != nil {
		c.logger.WarnContext(
			ctx,
			"Failed to render object",
			"object_id", objectId,
			"error", err,
		)
	}

	c.console.Printf(ctx, "\n")
}

// getObjectId extracts the ID from an object.
func (c *runnerContext) getObjectId(object proto.Message) string {
	// Use reflection to get the ID field
	msg := object.ProtoReflect()
	desc := msg.Descriptor()

	// Try to find the 'id' field
	idField := desc.Fields().ByName("id")
	if idField != nil {
		return msg.Get(idField).String()
	}

	return "<unknown>"
}
