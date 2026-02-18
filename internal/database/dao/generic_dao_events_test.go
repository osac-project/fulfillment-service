/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package dao

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
)

var _ = Describe("Generic DAO events", func() {
	var (
		ctx  context.Context
		pool *pgxpool.Pool
		tm   database.TxManager
	)

	BeforeEach(func() {
		var err error

		// Create a context:
		ctx = context.Background()

		// Prepare the database connection pool:
		db := server.MakeDatabase()
		DeferCleanup(db.Close)
		pool, err = pgxpool.New(ctx, db.MakeURL())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)

		// Prepare the transaction manager:
		tm, err = database.NewTxManager().
			SetLogger(logger).
			SetPool(pool).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create the tables:
		tx, err := tm.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		txCtx := database.TxIntoContext(ctx, tx)
		err = CreateTables(txCtx, "clusters")
		Expect(err).ToNot(HaveOccurred())
		err = tm.End(ctx, tx)
		Expect(err).ToNot(HaveOccurred())
	})

	// runWithTx starts a transaction, runs the given function using it, and ends the transaction when it finishes.
	runWithTx := func(task func(ctx context.Context)) {
		tx, err := tm.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		taskCtx := database.TxIntoContext(ctx, tx)
		task(taskCtx)
		err = tm.End(ctx, tx)
		Expect(err).ToNot(HaveOccurred())
	}

	It("Runs callback for create event", func() {
		var event *Event
		generic, err := NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			AddEventCallback(func(_ context.Context, e Event) error {
				event = &e
				return nil
			}).
			Build()
		Expect(err).ToNot(HaveOccurred())

		runWithTx(func(ctx context.Context) {
			_, err = generic.Create().SetObject(&privatev1.Cluster{}).Do(ctx)
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(err).ToNot(HaveOccurred())
		Expect(event).ToNot(BeNil())
		Expect(event.Table).To(Equal("clusters"))
		Expect(event.Type).To(Equal(EventTypeCreated))
	})

	It("Runs callback for modify event", func() {
		var event *Event
		generic, err := NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			AddEventCallback(func(_ context.Context, e Event) error {
				event = &e
				return nil
			}).
			Build()
		Expect(err).ToNot(HaveOccurred())

		var object *privatev1.Cluster
		runWithTx(func(ctx context.Context) {
			response, createErr := generic.Create().SetObject(&privatev1.Cluster{}).Do(ctx)
			err = createErr
			if err == nil {
				object = response.GetObject()
			}
		})
		Expect(err).ToNot(HaveOccurred())

		runWithTx(func(ctx context.Context) {
			_, err = generic.Update().SetObject(&privatev1.Cluster{
				Id: object.Id,
				Status: &privatev1.ClusterStatus{
					ApiUrl: "https://api.example.com",
				},
			}).Do(ctx)
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(event).ToNot(BeNil())
		Expect(event.Table).To(Equal("clusters"))
		Expect(event.Type).To(Equal(EventTypeUpdated))
	})

	It("Runs callback for delete event", func() {
		var event *Event
		generic, err := NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			AddEventCallback(func(_ context.Context, e Event) error {
				event = &e
				return nil
			}).
			Build()
		Expect(err).ToNot(HaveOccurred())

		var object *privatev1.Cluster
		runWithTx(func(ctx context.Context) {
			response, createErr := generic.Create().SetObject(&privatev1.Cluster{}).Do(ctx)
			err = createErr
			if err == nil {
				object = response.GetObject()
			}
		})
		Expect(err).ToNot(HaveOccurred())
		runWithTx(func(ctx context.Context) {
			_, err = generic.Delete().SetId(object.GetId()).Do(ctx)
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(event).ToNot(BeNil())
		Expect(event.Table).To(Equal("clusters"))
		Expect(event.Type).To(Equal(EventTypeDeleted))
	})

	It("Fails to create object if callback returns an error", func() {
		generic, err := NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			AddEventCallback(func(context.Context, Event) error {
				return errors.New("my error")
			}).
			Build()
		Expect(err).ToNot(HaveOccurred())
		var object *privatev1.Cluster
		runWithTx(func(ctx context.Context) {
			response, createErr := generic.Create().SetObject(&privatev1.Cluster{}).Do(ctx)
			err = createErr
			if err == nil {
				object = response.GetObject()
			}
		})
		Expect(err).To(MatchError("my error"))
		Expect(object).To(BeNil())
		row := pool.QueryRow(ctx, "select count(*) from clusters")
		var count int
		err = row.Scan(&count)
		Expect(err).ToNot(HaveOccurred())
		Expect(count).To(BeZero())
	})

	It("Fails to delete object if callback returns an error", func() {
		// Create the DAO, without callbacks, just to do the insert:
		generic, err := NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		var object *privatev1.Cluster
		runWithTx(func(ctx context.Context) {
			response, createErr := generic.Create().SetObject(&privatev1.Cluster{}).Do(ctx)
			err = createErr
			if err == nil {
				object = response.GetObject()
			}
		})
		Expect(err).ToNot(HaveOccurred())

		// Create the DAO again, this time with the callback, to do the delete:
		generic, err = NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			AddEventCallback(func(context.Context, Event) error {
				return errors.New("my error")
			}).
			Build()
		Expect(err).ToNot(HaveOccurred())
		runWithTx(func(ctx context.Context) {
			_, err = generic.Delete().SetId(object.GetId()).Do(ctx)
		})
		Expect(err).To(MatchError("my error"))

		// Check that the object is still there:
		var exists bool
		runWithTx(func(ctx context.Context) {
			response, existsErr := generic.Exists().SetId(object.GetId()).Do(ctx)
			err = existsErr
			if err == nil {
				exists = response.GetExists()
			}
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeTrue())
	})

	It("Doesn't fire update event if there are no changes", func() {
		// Create the DAO again:
		called := false
		generic, err := NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			AddEventCallback(func(_ context.Context, event Event) error {
				if event.Type == EventTypeUpdated {
					called = true
				}
				return nil
			}).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create the object:
		var object *privatev1.Cluster
		runWithTx(func(ctx context.Context) {
			response, createErr := generic.Create().SetObject(&privatev1.Cluster{}).Do(ctx)
			err = createErr
			if err == nil {
				object = response.GetObject()
			}
		})
		Expect(err).ToNot(HaveOccurred())

		// Update without changes and verify the result:
		runWithTx(func(ctx context.Context) {
			_, err = generic.Update().SetObject(object).Do(ctx)
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(called).To(BeFalse())
	})

	It("Fails to update object if callback returns an error", func() {
		// Create the DAO, without callbacks, just to do the insert:
		generic, err := NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
		var object *privatev1.Cluster
		runWithTx(func(ctx context.Context) {
			response, createErr := generic.Create().SetObject(&privatev1.Cluster{
				Status: &privatev1.ClusterStatus{
					ApiUrl: "https://my.api",
				},
			}).Do(ctx)
			err = createErr
			if err == nil {
				object = response.GetObject()
			}
		})
		Expect(err).ToNot(HaveOccurred())

		// Create the DAO again, this time with the callback, to do the update:
		generic, err = NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			AddEventCallback(func(_ context.Context, _ Event) error {
				return errors.New("my error")
			}).
			Build()
		Expect(err).ToNot(HaveOccurred())
		runWithTx(func(ctx context.Context) {
			_, err = generic.Update().SetObject(&privatev1.Cluster{
				Id: object.GetId(),
				Status: &privatev1.ClusterStatus{
					ApiUrl: "https://your.api",
				},
			}).Do(ctx)
		})
		Expect(err).To(MatchError("my error"))

		// Check that the object hasn't been updated:
		runWithTx(func(ctx context.Context) {
			getResponse, getErr := generic.Get().
				SetId(object.GetId()).
				Do(ctx)
			Expect(getErr).ToNot(HaveOccurred())
			err = getErr
			if err == nil {
				object = getResponse.GetObject()
			}
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(object).ToNot(BeNil())
		Expect(object.Status).ToNot(BeNil())
		Expect(object.Status.ApiUrl).To(Equal("https://my.api"))
	})

	It("Calls multiple callbacks", func() {
		called1 := false
		called2 := false
		generic, err := NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			AddEventCallback(func(context.Context, Event) error {
				called1 = true
				return nil
			}).
			AddEventCallback(func(context.Context, Event) error {
				called2 = true
				return nil
			}).
			Build()
		Expect(err).ToNot(HaveOccurred())

		runWithTx(func(ctx context.Context) {
			_, err = generic.Create().SetObject(&privatev1.Cluster{}).Do(ctx)
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(called1).To(BeTrue())
		Expect(called2).To(BeTrue())
	})

	It("Doesn't call second callback if first returns an error", func() {
		called1 := false
		called2 := false
		generic, err := NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			AddEventCallback(func(context.Context, Event) error {
				called1 = true
				return errors.New("my error 1")
			}).
			AddEventCallback(func(context.Context, Event) error {
				called2 = true
				return errors.New("my error 2")
			}).
			Build()
		Expect(err).ToNot(HaveOccurred())

		runWithTx(func(ctx context.Context) {
			_, err = generic.Create().SetObject(&privatev1.Cluster{}).Do(ctx)
		})
		Expect(err).To(MatchError("my error 1"))
		Expect(called1).To(BeTrue())
		Expect(called2).To(BeFalse())
	})

	It("Fires update event when deleting object with finalizers", func() {
		// Create a DAO that an event callback that saves the events:
		events := []Event{}
		generic, err := NewGenericDAO[*privatev1.Cluster]().
			SetLogger(logger).
			SetTable("clusters").
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			AddEventCallback(func(_ context.Context, event Event) error {
				events = append(events, event)
				return nil
			}).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create an object with finalizers:
		var object *privatev1.Cluster
		runWithTx(func(ctx context.Context) {
			createResponse, err := generic.Create().
				SetObject(
					privatev1.Cluster_builder{
						Metadata: privatev1.Metadata_builder{
							Finalizers: []string{
								"my-finalizer",
							},
						}.Build(),
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = createResponse.GetObject()
		})

		// Delete the object:
		runWithTx(func(ctx context.Context) {
			_, err = generic.Delete().
				SetId(object.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
		})

		// Remove the finalizers:
		object.Metadata.Finalizers = []string{}
		runWithTx(func(ctx context.Context) {
			_, err = generic.Update().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
		})

		// This should have generated four events:
		//
		// - The first for the creation of the object.
		// - The second one for the delete, which is translated into an update because there are finalizers.
		// - The third one for the update of the object that removes the finalizers.
		// - The fourth one for the automatic deletion of the object because finalizers have been removed.
		Expect(events).To(HaveLen(4))
		Expect(events[0].Type).To(Equal(EventTypeCreated))
		Expect(events[1].Type).To(Equal(EventTypeUpdated))
		Expect(events[2].Type).To(Equal(EventTypeUpdated))
		Expect(events[3].Type).To(Equal(EventTypeDeleted))
	})
})
