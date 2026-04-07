/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package coordination

import (
	"context"
	"net"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/servers"
)

var _ = Describe("Leader elector", func() {
	var (
		ctx    context.Context
		client *grpc.ClientConn
	)

	BeforeEach(func() {
		var err error

		// Create a context and remember to cancel it:
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		DeferCleanup(cancel)

		// Create a database:
		db := dbServer.MakeDatabase()
		DeferCleanup(db.Close)
		pool, err := pgxpool.New(ctx, db.MakeURL())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)

		// Create the transaction manager:
		txManager, err := database.NewTxManager().
			SetLogger(logger).
			SetPool(pool).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create the tables inside a transaction:
		tx, err := txManager.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		txCtx := database.TxIntoContext(ctx, tx)
		err = dao.CreateTables[*privatev1.Lease](txCtx)
		Expect(err).ToNot(HaveOccurred())
		err = txManager.End(txCtx, tx)
		Expect(err).ToNot(HaveOccurred())

		// Create the attribution and tenancy logic:
		attribution, err := auth.NewSystemAttributionLogic().
			SetLogger(logger).
			Build()
		Expect(err).ToNot(HaveOccurred())

		tenancy, err := auth.NewSystemTenancyLogic().
			SetLogger(logger).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create the tx interceptor:
		txInterceptor, err := database.NewTxInterceptor().
			SetLogger(logger).
			SetManager(txManager).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create the leases server:
		leasesServer, err := servers.NewPrivateLeasesServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create and start the gRPC server with the tx interceptor:
		grpcServer := grpc.NewServer(
			grpc.ChainUnaryInterceptor(txInterceptor.UnaryServer),
		)
		privatev1.RegisterLeasesServer(grpcServer, leasesServer)

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())

		go func() {
			defer GinkgoRecover()
			_ = grpcServer.Serve(listener)
		}()
		DeferCleanup(grpcServer.Stop)

		// Create the gRPC client:
		client, err = grpc.NewClient(
			listener.Addr().String(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { client.Close() })
	})

	Describe("Creation", func() {
		It("Can be built with all required parameters", func() {
			elector, err := NewLeaderElector().
				SetLogger(logger).
				SetClient(client).
				SetLeaseId("my-lease").
				SetLeaseHolder("my-holder").
				SetCallback(func(ctx context.Context) {}).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(elector).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			_, err := NewLeaderElector().
				SetClient(client).
				SetLeaseId("my-lease").
				SetLeaseHolder("my-holder").
				SetCallback(func(ctx context.Context) {}).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
		})

		It("Fails if client is not set", func() {
			_, err := NewLeaderElector().
				SetLogger(logger).
				SetLeaseId("my-lease").
				SetLeaseHolder("my-holder").
				SetCallback(func(ctx context.Context) {}).
				Build()
			Expect(err).To(MatchError("client is mandatory"))
		})

		It("Fails if identifier is not set", func() {
			_, err := NewLeaderElector().
				SetLogger(logger).
				SetClient(client).
				SetLeaseHolder("my-holder").
				SetCallback(func(ctx context.Context) {}).
				Build()
			Expect(err).To(MatchError("identifier is mandatory"))
		})

		It("Fails if holder is not set", func() {
			_, err := NewLeaderElector().
				SetLogger(logger).
				SetClient(client).
				SetLeaseId("my-lease").
				SetCallback(func(ctx context.Context) {}).
				Build()
			Expect(err).To(MatchError("holder is mandatory"))
		})

		It("Fails if callback is not set", func() {
			_, err := NewLeaderElector().
				SetLogger(logger).
				SetClient(client).
				SetLeaseId("my-lease").
				SetLeaseHolder("my-holder").
				Build()
			Expect(err).To(MatchError("callback is mandatory"))
		})
	})

	Describe("Behaviour", func() {
		It("Becomes leader when no lease exists", func() {
			leading := make(chan struct{})
			callback := func(ctx context.Context) {
				close(leading)
				<-ctx.Done()
			}
			elector, err := NewLeaderElector().
				SetLogger(logger).
				SetClient(client).
				SetLeaseId("my-lease").
				SetLeaseHolder("my-holder").
				SetLeaseDuration(5 * time.Second).
				SetCallback(callback).
				Build()
			Expect(err).ToNot(HaveOccurred())
			go elector.Run(ctx)
			Eventually(leading).Should(BeClosed())
		})

		It("Only one of two concurrent instances becomes leader", func() {
			// Here we will count the number of simultaneously leading instances:
			count := &atomic.Int32{}

			// First instance:
			leading1 := make(chan struct{})
			callback1 := func(ctx context.Context) {
				count.Add(1)
				close(leading1)
				<-ctx.Done()
			}
			elector1, err := NewLeaderElector().
				SetLogger(logger).
				SetClient(client).
				SetLeaseId("my-lease").
				SetLeaseHolder("my-holder-1").
				SetLeaseDuration(5 * time.Second).
				SetCallback(callback1).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Second instance:
			leading2 := make(chan struct{})
			callback2 := func(ctx context.Context) {
				count.Add(1)
				close(leading2)
				<-ctx.Done()
			}
			elector2, err := NewLeaderElector().
				SetLogger(logger).
				SetClient(client).
				SetLeaseId("my-lease").
				SetLeaseHolder("my-holder-2").
				SetLeaseDuration(5 * time.Second).
				SetCallback(callback2).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Start the instances:
			go elector1.Run(ctx)
			go elector2.Run(ctx)

			// Wait until at least one becomes leader:
			Eventually(
				func(g Gomega) {
					g.Expect(count.Load()).To(BeNumerically(">=", 1))
				},
			).Should(Succeed())

			// Give some time for a potential second leader to appear (it shouldn't):
			Consistently(
				func(g Gomega) {
					g.Expect(count.Load()).To(BeNumerically("==", 1))
				},
				2*time.Second,
			).Should(Succeed())
		})

		It("Second instance takes over after lease expires", func() {
			// First instance:
			leading1 := make(chan struct{})
			done1 := make(chan struct{})
			callback1 := func(ctx context.Context) {
				close(leading1)
				<-ctx.Done()
				close(done1)
			}
			elector1, err := NewLeaderElector().
				SetLogger(logger).
				SetClient(client).
				SetLeaseId("my-lease").
				SetLeaseHolder("my-holder-1").
				SetLeaseDuration(2 * time.Second).
				SetCallback(callback1).
				Build()
			Expect(err).ToNot(HaveOccurred())
			ctx1, cancel1 := context.WithCancel(ctx)
			defer cancel1()
			go elector1.Run(ctx1)

			// Wait until the first instance becomes leader:
			Eventually(leading1).Should(BeClosed())

			// Stop the first instance so that it stops renewing the lease:
			cancel1()
			Eventually(done1).Should(BeClosed())

			// Second instance competing for the same lease:
			leading2 := make(chan struct{})
			callback2 := func(ctx context.Context) {
				close(leading2)
				<-ctx.Done()
			}
			elector2, err := NewLeaderElector().
				SetLogger(logger).
				SetClient(client).
				SetLeaseId("my-lease").
				SetLeaseHolder("my-holder-2").
				SetLeaseDuration(5 * time.Second).
				SetCallback(callback2).
				Build()
			Expect(err).ToNot(HaveOccurred())
			ctx2, cancel2 := context.WithCancel(ctx)
			defer cancel2()
			go elector2.Run(ctx2)

			// Wait until the second instance becomes leader:
			Eventually(leading2, 10*time.Second).Should(BeClosed())
		})
	})
})
