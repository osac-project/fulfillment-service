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
	"io"
	"net"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
	"github.com/osac-project/fulfillment-service/internal/events"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

// eventsCollector reads events from a Watch stream in the background and collects them for later assertions.
type eventsCollector struct {
	lock   sync.Mutex
	events []*publicv1.Event
}

// Collect reads events from the stream in a goroutine until the stream ends or the context is canceled.
func (c *eventsCollector) Collect(stream publicv1.Events_WatchClient) {
	go func() {
		defer GinkgoRecover()
		for {
			response, err := stream.Recv()
			if errors.Is(err, io.EOF) || err != nil {
				return
			}
			c.lock.Lock()
			c.events = append(c.events, response.GetEvent())
			c.lock.Unlock()
		}
	}()
}

// Events returns a snapshot of collected events, safe for use with Eventually/Consistently.
func (c *eventsCollector) Events() []*publicv1.Event {
	c.lock.Lock()
	defer c.lock.Unlock()
	result := make([]*publicv1.Event, len(c.events))
	copy(result, c.events)
	return result
}

var _ = Describe("Events server visibility", func() {
	var (
		ctrl     *gomock.Controller
		listener *events.MockListener
		callback events.Callback
	)

	BeforeEach(func() {
		// Create the mock controller:
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)

		// Create a mock listener that captures the callback and blocks until the context is canceled:
		listener = events.NewMockListener(ctrl)
		listener.EXPECT().Listen(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, cb events.Callback) error {
				callback = cb
				<-ctx.Done()
				return ctx.Err()
			},
		).AnyTimes()
	})

	// sendEvent delivers an event directly through the captured listener callback.
	sendEvent := func(event *privatev1.Event) {
		err := callback(context.Background(), event)
		Expect(err).ToNot(HaveOccurred())
	}

	// makeEvent builds a private event with the given tenant and project.
	makeEvent := func(tenant string) *privatev1.Event {
		return privatev1.Event_builder{
			Id:   uuid.New(),
			Type: privatev1.EventType_EVENT_TYPE_OBJECT_CREATED,
			Cluster: privatev1.Cluster_builder{
				Id: uuid.New(),
				Metadata: privatev1.Metadata_builder{
					Tenant: tenant,
				}.Build(),
			}.Build(),
		}.Build()
	}

	// startServer creates an events server with the mock listener and given tenancy, starts it behind a bufconn
	// gRPC server, and returns a connected client.
	startServer := func(tenancy auth.TenancyLogic) publicv1.EventsClient {
		// Create the events server:
		eventsServer, err := NewEventsServer().
			SetLogger(logger).
			SetListener(listener).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Start the events server in the background:
		eventsCtx, eventsCancel := context.WithCancel(context.Background())
		go func() {
			defer GinkgoRecover()
			_ = eventsServer.Start(eventsCtx)
		}()
		DeferCleanup(eventsCancel)

		// Create the gRPC server using bufconn:
		grpcListener := bufconn.Listen(1024 * 1024)
		grpcServer := grpc.NewServer()
		publicv1.RegisterEventsServer(grpcServer, eventsServer)
		go func() {
			defer GinkgoRecover()
			_ = grpcServer.Serve(grpcListener)
		}()
		DeferCleanup(grpcServer.Stop)

		// Create the gRPC connection using bufconn:
		grpcDialer := func(context.Context, string) (result net.Conn, err error) {
			result, err = grpcListener.Dial()
			return
		}
		grpcCredentials := insecure.NewCredentials()
		grpcConnection, err := grpc.NewClient(
			"passthrough://bufnet",
			grpc.WithContextDialer(grpcDialer),
			grpc.WithTransportCredentials(grpcCredentials),
		)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(grpcConnection.Close)

		return publicv1.NewEventsClient(grpcConnection)
	}

	// makeTenancy creates a mock tenancy logic returning the given visible tenants:
	makeTenancy := func(tenants collections.Set[string]) *auth.MockTenancyLogic {
		mock := auth.NewMockTenancyLogic(ctrl)
		mock.EXPECT().DetermineVisibleTenants(gomock.Any()).
			Return(tenants, nil).
			AnyTimes()
		return mock
	}

	// startWatch opens a Watch stream and returns a collector that accumulates received events.
	startWatch := func(client publicv1.EventsClient) (collector *eventsCollector, cancel context.CancelFunc) {
		watchCtx, watchCancel := context.WithCancel(context.Background())
		stream, err := client.Watch(watchCtx, publicv1.EventsWatchRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		collector = &eventsCollector{}
		collector.Collect(stream)
		return collector, watchCancel
	}

	It("Delivers events when tenant is visible", func() {
		client := startServer(makeTenancy(
			collections.NewSet("tenant-a"),
		))
		collector, cancel := startWatch(client)
		defer cancel()
		Eventually(
			func() int {
				sendEvent(makeEvent("tenant-a"))
				return len(collector.Events())
			},
			10*time.Second,
		).Should(BeNumerically(">=", 1))
	})

	It("Filters out events when tenant is not visible", func() {
		client := startServer(makeTenancy(
			collections.NewSet("tenant-b"),
		))
		collector, cancel := startWatch(client)
		defer cancel()
		sendEvent(makeEvent("tenant-a"))
		Consistently(collector.Events, time.Second).Should(BeEmpty())
	})

	It("Delivers only visible events when multiple are sent", func() {
		client := startServer(makeTenancy(
			collections.NewSet("tenant-a"),
		))
		collector, cancel := startWatch(client)
		defer cancel()
		Eventually(
			func() int {
				sendEvent(makeEvent("tenant-a"))
				sendEvent(makeEvent("tenant-b"))
				return len(collector.Events())
			},
			10*time.Second,
		).Should(BeNumerically("==", 1))
	})
})
