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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/timestamppb"

	testsv1 "github.com/osac-project/fulfillment-service/internal/api/tests/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/collections"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("Generic DAO", func() {
	const (
		defaultLimit = 5
		maxLimit     = 10
		objectCount  = maxLimit + 1
	)

	var (
		ctx context.Context
		tx  database.Tx
	)

	sort := func(objects []*testsv1.Object) {
		sort.Slice(objects, func(i, j int) bool {
			return strings.Compare(objects[i].GetId(), objects[j].GetId()) < 0
		})
	}

	BeforeEach(func() {
		var err error

		// Create a context:
		ctx = context.Background()

		// Prepare the database pool:
		db := server.MakeDatabase()
		DeferCleanup(db.Close)
		pool, err := pgxpool.New(ctx, db.MakeURL())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)

		// Create the transaction manager:
		tm, err := database.NewTxManager().
			SetLogger(logger).
			SetPool(pool).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Start a transaction and add it to the context:
		tx, err = tm.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := tm.End(ctx, tx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			generic, err := NewGenericDAO[*testsv1.Object]().
				SetLogger(logger).
				SetTable("objects").
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(generic).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			generic, err := NewGenericDAO[*testsv1.Object]().
				SetTable("objects").
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(generic).To(BeNil())
		})

		It("Fails if table is not set", func() {
			generic, err := NewGenericDAO[*testsv1.Object]().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("table is mandatory"))
			Expect(generic).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			generic, err := NewGenericDAO[*testsv1.Object]().
				SetLogger(logger).
				SetTable("objects").
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("attribution logic is mandatory"))
			Expect(generic).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			generic, err := NewGenericDAO[*testsv1.Object]().
				SetLogger(logger).
				SetTable("objects").
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(generic).To(BeNil())
		})

		It("Fails if default limit is zero", func() {
			generic, err := NewGenericDAO[*testsv1.Object]().
				SetLogger(logger).
				SetTable("objects").
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetDefaultLimit(0).
				Build()
			Expect(err).To(MatchError("default limit must be a possitive integer, but it is 0"))
			Expect(generic).To(BeNil())
		})

		It("Fails if default limit is negative", func() {
			generic, err := NewGenericDAO[*testsv1.Object]().
				SetLogger(logger).
				SetTable("objects").
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetDefaultLimit(-1).
				Build()
			Expect(err).To(MatchError("default limit must be a possitive integer, but it is -1"))
			Expect(generic).To(BeNil())
		})

		It("Fails if max limit is zero", func() {
			generic, err := NewGenericDAO[*testsv1.Object]().
				SetLogger(logger).
				SetTable("objects").
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetMaxLimit(0).
				Build()
			Expect(err).To(MatchError("max limit must be a possitive integer, but it is 0"))
			Expect(generic).To(BeNil())
		})

		It("Fails if max limit is negative", func() {
			generic, err := NewGenericDAO[*testsv1.Object]().
				SetLogger(logger).
				SetTable("objects").
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetMaxLimit(-1).
				Build()
			Expect(err).To(MatchError("max limit must be a possitive integer, but it is -1"))
			Expect(generic).To(BeNil())
		})

		It("Fails if max limit is less than default limit", func() {
			generic, err := NewGenericDAO[*testsv1.Object]().
				SetLogger(logger).
				SetTable("objects").
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				SetMaxLimit(100).
				SetDefaultLimit(1000).
				Build()
			Expect(err).To(MatchError(
				"max limit must be greater or equal to default limit, but max limit is 100 and " +
					"default limit is 1000",
			))
			Expect(generic).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var generic *GenericDAO[*testsv1.Object]

		BeforeEach(func() {
			// Create the tables:
			err := CreateTables(ctx, "objects")
			Expect(err).ToNot(HaveOccurred())

			// Create the attribution logic:
			attributionLogic := auth.NewMockAttributionLogic(ctrl)
			attributionLogic.EXPECT().DetermineAssignedCreators(gomock.Any()).
				Return(
					collections.NewSet("my_user"),
					nil,
				).
				AnyTimes()

			// Create the tenancy logic:
			tenancyLogic := auth.NewMockTenancyLogic(ctrl)
			tenancyLogic.EXPECT().DetermineAssignableTenants(gomock.Any()).
				Return(
					collections.NewSet("my_tenant"),
					nil,
				).
				AnyTimes()
			tenancyLogic.EXPECT().DetermineDefaultTenants(gomock.Any()).
				Return(
					collections.NewSet("my_tenant"),
					nil,
				).
				AnyTimes()
			tenancyLogic.EXPECT().DetermineVisibleTenants(gomock.Any()).
				Return(
					collections.NewSet("my_tenant"),
					nil,
				).
				AnyTimes()

			// Create the DAO:
			generic, err = NewGenericDAO[*testsv1.Object]().
				SetLogger(logger).
				SetTable("objects").
				SetDefaultLimit(defaultLimit).
				SetMaxLimit(maxLimit).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancyLogic).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Creates object", func() {
			object := &testsv1.Object{}
			createResponse, err := generic.Create().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			created := createResponse.GetObject()
			getResponse, err := generic.Get().SetId(created.GetId()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			result := getResponse.GetObject()
			Expect(result).ToNot(BeNil())
		})

		It("Creates empty object if no object is provided", func() {
			createResponse, err := generic.Create().Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			created := createResponse.GetObject()
			Expect(created).ToNot(BeNil())
			Expect(created.GetId()).ToNot(BeEmpty())
			getResponse, err := generic.Get().SetId(created.GetId()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			result := getResponse.GetObject()
			Expect(result).ToNot(BeNil())
			Expect(result.GetId()).To(Equal(created.GetId()))
		})

		It("Sets metadata when creating", func() {
			object := &testsv1.Object{}
			response, err := generic.Create().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			result := response.GetObject()
			Expect(result.Metadata).ToNot(BeNil())
		})

		It("Sets creators when creating", func() {
			// Create the object and verify that the result has the creator set:
			object := &testsv1.Object{}
			response, err := generic.Create().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = response.GetObject()
			Expect(object.GetMetadata().GetCreators()).To(ConsistOf("my-user"))

			// Get the object and verify that the result has the creators set:
			getResponse, err := generic.Get().
				SetId(object.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetMetadata().GetCreators()).To(ConsistOf("my-user"))
		})

		It("Sets creation timestamp when creating", func() {
			object := &testsv1.Object{}
			response, err := generic.Create().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			result := response.GetObject()
			Expect(result).ToNot(BeNil())
			Expect(result.Metadata).ToNot(BeNil())
			Expect(result.Metadata.CreationTimestamp).ToNot(BeNil())
			Expect(result.Metadata.CreationTimestamp.AsTime()).ToNot(BeZero())
		})

		It("Doesn't set deletion timestamp when creating", func() {
			response, err := generic.Create().Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.Metadata).ToNot(BeNil())
			Expect(object.Metadata.DeletionTimestamp).To(BeNil())
		})

		It("Sets name when creating", func() {
			// Create the object with a name and verify that the result has the name set:
			object := &testsv1.Object{
				Metadata: &testsv1.Metadata{
					Name: "my-name",
				},
			}
			response, err := generic.Create().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = response.GetObject()
			Expect(object.GetMetadata().GetName()).To(Equal("my-name"))

			// Get the object and verify that the result has the name set:
			getResponse, err := generic.Get().SetId(object.GetId()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetMetadata().GetName()).To(Equal("my-name"))
		})

		It("Sets labels when creating", func() {
			object := &testsv1.Object{
				Metadata: &testsv1.Metadata{
					Labels: map[string]string{
						"my-label": "my-value",
					},
				},
			}
			response, err := generic.Create().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = response.GetObject()
			Expect(object.GetMetadata().GetLabels()).To(Equal(map[string]string{
				"my-label": "my-value",
			}))

			getResponse, err := generic.Get().SetId(object.GetId()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetMetadata().GetLabels()).To(Equal(map[string]string{
				"my-label": "my-value",
			}))
		})

		It("Sets annotations when creating", func() {
			object := &testsv1.Object{
				Metadata: &testsv1.Metadata{
					Annotations: map[string]string{
						"my-annotation": "my-value",
					},
				},
			}
			response, err := generic.Create().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = response.GetObject()
			annotations := object.GetMetadata().GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("my-annotation", "my-value"))

			getResponse, err := generic.Get().SetId(object.GetId()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			annotations = object.GetMetadata().GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("my-annotation", "my-value"))
		})

		It("Generates non empty identifiers", func() {
			response, err := generic.Create().Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())
		})

		It("Doesn't put the generated identifier inside the input object", func() {
			object := &testsv1.Object{}
			_, err := generic.Create().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(object.GetId()).To(BeEmpty())
		})

		It("Doesn't put the generated metadata inside the input object", func() {
			object := &testsv1.Object{}
			_, err := generic.Create().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(object.Metadata).To(BeNil())
		})

		It("Returns 'already exists' when creating object with existing identifier", func() {
			// Create an object with a specific identifier:
			id := uuid.New()
			_, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						Id: id,
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Try to create another object with the same identifier:
			_, err = generic.Create().
				SetObject(
					testsv1.Object_builder{
						Id: id,
					}.Build(),
				).
				Do(ctx)
			Expect(err).To(HaveOccurred())
			var alreadyExistsErr *ErrAlreadyExists
			Expect(errors.As(err, &alreadyExistsErr)).To(BeTrue())
			Expect(alreadyExistsErr.ID).To(Equal(id))
		})

		It("Gets object", func() {
			createResponse, err := generic.Create().Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			getResponse, err := generic.Get().
				SetId(object.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			result := getResponse.GetObject()
			Expect(result).ToNot(BeNil())
		})

		It("Returns not found error when getting object that doesn't exist", func() {
			_, err := generic.Get().
				SetId("does-not-exist").
				Do(ctx)
			Expect(err).To(HaveOccurred())
			var notFoundErr *ErrNotFound
			Expect(errors.As(err, &notFoundErr)).To(BeTrue())
			Expect(notFoundErr.ID).To(Equal("does-not-exist"))
		})

		It("Lists objects", func() {
			// Insert a couple of rows:
			const count = 2
			for range count {
				_, err := generic.Create().Do(ctx)
				Expect(err).ToNot(HaveOccurred())
			}

			// Try to list:
			response, err := generic.List().Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(count))
			for _, item := range response.GetItems() {
				Expect(item).ToNot(BeNil())
			}
		})

		It("Lists objects sorted by ID identifier default", func() {
			// Create objects with specific identifier in non-alphabetical order:
			ids := []string{"zebra", "apple", "banana"}
			objects := make([]*testsv1.Object, len(ids))
			for i, id := range ids {
				createResponse, err := generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: id,
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				objects[i] = createResponse.GetObject()
			}

			// List objects and verify they are sorted by identifier:
			response, err := generic.List().Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(len(ids)))

			// Verify the objects are returned in alphabetical order:
			expected := []string{"apple", "banana", "zebra"}
			for i, item := range response.GetItems() {
				Expect(item.GetId()).To(Equal(expected[i]))
			}
		})

		It("Doesn't save the creation identifier in the 'data' column", func() {
			// Create an object:
			response, err := generic.Create().Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()

			// Verify that the database isn't stored in the 'data' column:
			row := tx.QueryRow(ctx, "select data from objects where id = $1", object.GetId())
			var data []byte
			err = row.Scan(&data)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(data)).ToNot(ContainSubstring(object.GetId()))
		})

		It("Doesn't save the creation timestamp in the 'data' column", func() {
			// Create an object:
			response, err := generic.Create().Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()

			// Verify that the database isn't stored in the 'data' column:
			row := tx.QueryRow(ctx, "select data from objects where id = $1", object.GetId())
			var data []byte
			err = row.Scan(&data)
			Expect(err).ToNot(HaveOccurred())
			var value map[string]any
			err = json.Unmarshal(data, &value)
			Expect(err).ToNot(HaveOccurred())
			Expect(value).ToNot(HaveKey("creation_timestamp"))
		})

		It("Archives object if it has no finalizers when it is deleted", func() {
			// Create an object without finalizers:
			response, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						MyString: "my value",
						MyBool:   true,
						MyInt32:  123,
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()

			// Delete the object:
			_, err = generic.Delete().
				SetId(object.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify that the object has been deleted and the data copied to the archive table:
			row := tx.QueryRow(ctx, `select count(*) from objects where id = $1`, object.GetId())
			var count int
			err = row.Scan(&count)
			Expect(err).ToNot(HaveOccurred())
			Expect(count).To(BeZero())
			row = tx.QueryRow(
				ctx,
				`
				select
					creation_timestamp,
					deletion_timestamp,
					archival_timestamp,
					data
				from
					archived_objects
				where
					id = $1
				`,
				object.GetId(),
			)
			var (
				creationTs time.Time
				deletionTs time.Time
				archivalTs time.Time
				data       []byte
			)
			err = row.Scan(
				&creationTs,
				&deletionTs,
				&archivalTs,
				&data,
			)
			Expect(err).ToNot(HaveOccurred())
			metadata := object.GetMetadata()
			now := time.Now()
			Expect(creationTs).To(BeTemporally("==", metadata.GetCreationTimestamp().AsTime()))
			Expect(deletionTs).To(BeTemporally("~", now, time.Second))
			Expect(archivalTs).To(BeTemporally("~", now, time.Second))
			Expect(data).To(MatchJSON(`{
				"my_string": "my value",
				"my_bool": true,
				"my_int32": 123
			}`))
		})

		It("Archives object when it is updated removing the finalizers", func() {
			// Create an object with finalizers:
			response, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						Metadata: testsv1.Metadata_builder{
							Finalizers: []string{"a"},
						}.Build(),
						MyString: "my value",
						MyBool:   true,
						MyInt32:  123,
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()

			// Delete the object:
			_, err = generic.Delete().
				SetId(object.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify that it hasn't been archived:
			row := tx.QueryRow(ctx, `select count(*) from archived_objects where id = $1`, object.GetId())
			var count int
			err = row.Scan(&count)
			Expect(err).ToNot(HaveOccurred())
			Expect(count).To(BeZero())

			// Update the object removing the finalizers:
			updateResponse, err := generic.Update().
				SetObject(
					testsv1.Object_builder{
						Id: object.GetId(),
						Metadata: testsv1.Metadata_builder{
							Finalizers: []string{},
						}.Build(),
						MyString: "your value",
						MyBool:   false,
						MyInt32:  456,
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()

			// Verify that it has been removed and copied to the archive table:
			row = tx.QueryRow(
				ctx,
				`
				select
					creation_timestamp,
					deletion_timestamp,
					archival_timestamp,
					data
				from
					archived_objects
				where
					id = $1
				`,
				object.GetId(),
			)
			var (
				creationTs time.Time
				deletionTs time.Time
				archivalTs time.Time
				data       []byte
			)
			err = row.Scan(
				&creationTs,
				&deletionTs,
				&archivalTs,
				&data,
			)
			Expect(err).ToNot(HaveOccurred())
			metadata := object.GetMetadata()
			now := time.Now()
			Expect(creationTs).To(BeTemporally("==", metadata.GetCreationTimestamp().AsTime()))
			Expect(deletionTs).To(BeTemporally("~", metadata.GetDeletionTimestamp().AsTime()))
			Expect(archivalTs).To(BeTemporally("~", now, time.Second))
			Expect(data).To(MatchJSON(`{
				"my_string": "your value",
				"my_int32": 456
			}`))
		})

		It("Copies labels and annotations when archived on delete", func() {
			// Create an object without finalizers:
			response, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						Metadata: testsv1.Metadata_builder{
							Labels: map[string]string{
								"my-label": "my-value",
							},
							Annotations: map[string]string{
								"my-annotation": "my-value",
							},
						}.Build(),
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()

			// Delete the object:
			_, err = generic.Delete().
				SetId(object.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify that labels and annotations were archived:
			row := tx.QueryRow(
				ctx,
				`
				select
					labels,
					annotations
				from
					archived_objects
				where
					id = $1
				`,
				object.GetId(),
			)
			var (
				labelsData      []byte
				annotationsData []byte
			)
			err = row.Scan(&labelsData, &annotationsData)
			Expect(err).ToNot(HaveOccurred())
			var labels map[string]string
			err = json.Unmarshal(labelsData, &labels)
			Expect(err).ToNot(HaveOccurred())
			Expect(labels).To(Equal(map[string]string{
				"my-label": "my-value",
			}))
			var annotations map[string]string
			err = json.Unmarshal(annotationsData, &annotations)
			Expect(err).ToNot(HaveOccurred())
			Expect(annotations).To(Equal(map[string]string{
				"my-annotation": "my-value",
			}))
		})

		It("Copies labels and annotations when archived on update", func() {
			// Create an object with finalizers:
			response, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						Metadata: testsv1.Metadata_builder{
							Finalizers: []string{"a"},
						}.Build(),
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()

			// Delete the object:
			_, err = generic.Delete().
				SetId(object.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Update the object removing finalizers and adding metadata:
			Expect(err).ToNot(HaveOccurred())
			_, err = generic.Update().
				SetObject(
					testsv1.Object_builder{
						Id: object.GetId(),
						Metadata: testsv1.Metadata_builder{
							Finalizers: []string{},
							Labels: map[string]string{
								"my-label": "my-value",
							},
							Annotations: map[string]string{
								"my-annotation": "my-value",
							},
						}.Build(),
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify that labels and annotations were archived:
			row := tx.QueryRow(
				ctx,
				`
				select
					labels,
					annotations
				from
					archived_objects
				where
					id = $1
				`,
				object.GetId(),
			)
			var (
				labelsData      []byte
				annotationsData []byte
			)
			err = row.Scan(&labelsData, &annotationsData)
			Expect(err).ToNot(HaveOccurred())
			var labels map[string]string
			err = json.Unmarshal(labelsData, &labels)
			Expect(err).ToNot(HaveOccurred())
			Expect(labels).To(Equal(map[string]string{
				"my-label": "my-value",
			}))
			var annotations map[string]string
			err = json.Unmarshal(annotationsData, &annotations)
			Expect(err).ToNot(HaveOccurred())
			Expect(annotations).To(Equal(map[string]string{
				"my-annotation": "my-value",
			}))
		})

		It("Returns not found error when deleting object that doesn't exist", func() {
			_, err := generic.Delete().
				SetId("does_not_exist").
				Do(ctx)
			Expect(err).To(HaveOccurred())
			var notFoundErr *ErrNotFound
			Expect(errors.As(err, &notFoundErr)).To(BeTrue())
			Expect(notFoundErr.ID).To(Equal("does_not_exist"))
		})

		Describe("Finalizers", func() {
			checkDatabase := func(object *testsv1.Object, expected ...string) {
				row := tx.QueryRow(ctx, "select finalizers from objects where id = $1", object.GetId())
				var actual []string
				err := row.Scan(&actual)
				Expect(err).ToNot(HaveOccurred())
				values := make([]any, len(expected))
				for i, value := range expected {
					values[i] = value
				}
				Expect(actual).To(ConsistOf(values))
			}

			It("Gets finalizers", func() {
				response, err := generic.Create().
					SetObject(testsv1.Object_builder{
						Metadata: testsv1.Metadata_builder{
							Finalizers: []string{"a", "b"},
						}.Build(),
					}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := response.GetObject()
				getResponse, err := generic.Get().
					SetId(object.GetId()).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object = getResponse.GetObject()
				Expect(object.GetMetadata().GetFinalizers()).To(ConsistOf("a", "b"))
			})

			It("Lists finalizers", func() {
				createResponse, err := generic.Create().
					SetObject(
						testsv1.Object_builder{
							Metadata: testsv1.Metadata_builder{
								Finalizers: []string{"a", "b"},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := createResponse.GetObject()
				listResponse, err := generic.List().
					SetFilter(fmt.Sprintf("this.id == '%s'", object.GetId())).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				objects := listResponse.GetItems()
				Expect(objects).To(HaveLen(1))
				object = objects[0]
				Expect(object.GetMetadata().GetFinalizers()).To(ConsistOf("a", "b"))
			})

			It("Creates object without finalizers", func() {
				response, err := generic.Create().
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := response.GetObject()
				Expect(object.GetMetadata().GetFinalizers()).To(BeEmpty())
				checkDatabase(object)
			})

			It("Creates object with one finalizer", func() {
				response, err := generic.Create().
					SetObject(
						testsv1.Object_builder{
							Metadata: testsv1.Metadata_builder{
								Finalizers: []string{"a"},
							}.Build(),
						}.Build()).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := response.GetObject()
				Expect(object.GetMetadata().GetFinalizers()).To(ConsistOf("a"))
				checkDatabase(object, "a")
			})

			It("Creates object with two finalizers", func() {
				response, err := generic.Create().
					SetObject(
						testsv1.Object_builder{
							Metadata: testsv1.Metadata_builder{
								Finalizers: []string{"a", "b"},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := response.GetObject()
				Expect(object.GetMetadata().GetFinalizers()).To(ConsistOf("a", "b"))
				checkDatabase(object, "a", "b")
			})

			It("Eliminates duplicated finalizers when object is created", func() {
				response, err := generic.Create().
					SetObject(
						testsv1.Object_builder{
							Metadata: testsv1.Metadata_builder{
								Finalizers: []string{"a", "a"},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := response.GetObject()
				Expect(object.GetMetadata().GetFinalizers()).To(ConsistOf("a"))
				checkDatabase(object, "a")
			})

			It("Adds one finalizer when object is updated", func() {
				response, err := generic.Create().
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := response.GetObject()
				object.GetMetadata().SetFinalizers([]string{"a"})
				updateResponse, err := generic.Update().
					SetObject(object).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object = updateResponse.GetObject()
				Expect(object.GetMetadata().GetFinalizers()).To(ConsistOf("a"))
				checkDatabase(object, "a")
			})

			It("Adds two finalizers when object is updated", func() {
				response, err := generic.Create().
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := response.GetObject()
				object.GetMetadata().SetFinalizers([]string{"a", "b"})
				updateResponse, err := generic.Update().
					SetObject(object).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object = updateResponse.GetObject()
				Expect(object.GetMetadata().GetFinalizers()).To(ConsistOf("a", "b"))
				checkDatabase(object, "a", "b")
			})

			It("Replaces finalizers when object is updated", func() {
				response, err := generic.Create().
					SetObject(
						testsv1.Object_builder{
							Metadata: testsv1.Metadata_builder{
								Finalizers: []string{"a", "b"},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := response.GetObject()
				object.GetMetadata().SetFinalizers([]string{"a", "c"})
				updateResponse, err := generic.Update().
					SetObject(object).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object = updateResponse.GetObject()
				Expect(object.GetMetadata().GetFinalizers()).To(ConsistOf("a", "c"))
				checkDatabase(object, "a", "c")
			})

			It("Eliminates duplicated finalizers when object is updated", func() {
				response, err := generic.Create().
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := response.GetObject()
				object.GetMetadata().SetFinalizers([]string{"a", "a"})
				updateResponse, err := generic.Update().
					SetObject(object).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object = updateResponse.GetObject()
				Expect(object.GetMetadata().GetFinalizers()).To(ConsistOf("a"))
				checkDatabase(object, "a")
			})
		})

		Describe("Paging", func() {
			var objects []*testsv1.Object

			BeforeEach(func() {
				// Create a list of objects and sort it like they will be sorted by the DAO. Not that
				// this works correctly because the DAO sorts object by identifier by default.
				objects = make([]*testsv1.Object, objectCount)
				for i := range len(objects) {
					objects[i] = &testsv1.Object{
						Id: uuid.New(),
					}
					_, err := generic.Create().
						SetObject(objects[i]).
						Do(ctx)
					Expect(err).ToNot(HaveOccurred())
				}
				sort(objects)
			})

			It("Uses zero as default offset", func() {
				response, err := generic.List().
					SetLimit(1).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetItems()[0].Id).To(Equal(objects[0].Id))
			})

			It("Honours valid offset", func() {
				for i := range len(objects) {
					response, err := generic.List().
						SetOffset(int32(i)).
						SetLimit(1).
						Do(ctx)
					Expect(err).ToNot(HaveOccurred())
					Expect(response.GetItems()[0].Id).To(Equal(objects[i].Id))
				}
			})

			It("Returns empty list if offset is greater or equal than available items", func() {
				response, err := generic.List().
					SetOffset(objectCount).
					SetLimit(1).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetItems()).To(BeEmpty())
			})

			It("Ignores negative offset", func() {
				response, err := generic.List().
					SetOffset(-123).
					SetLimit(1).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetItems()[0].Id).To(Equal(objects[0].Id))
			})

			It("Interprets negative limit as requesting zero items", func() {
				response, err := generic.List().
					SetLimit(-123).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeZero())
				Expect(response.GetItems()).To(BeEmpty())
			})

			It("Interprets zero limit as requesting the default number of items", func() {
				response, err := generic.List().
					SetLimit(0).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", defaultLimit))
				Expect(response.GetItems()).To(HaveLen(defaultLimit))
			})

			It("Truncates limit to the maximum", func() {
				response, err := generic.List().
					SetLimit(maxLimit + 1).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", maxLimit))
				Expect(response.GetItems()).To(HaveLen(maxLimit))
			})

			It("Honours valid limit", func() {
				for i := 1; i < maxLimit; i++ {
					response, err := generic.List().
						SetLimit(int32(i)).
						Do(ctx)
					Expect(err).ToNot(HaveOccurred())
					Expect(response.GetSize()).To(BeNumerically("==", i))
					Expect(response.GetItems()).To(HaveLen(i))
				}
			})

			It("Returns less items than requested if there are not enough", func() {
				response, err := generic.List().
					SetOffset(objectCount - 2).
					SetLimit(10).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 2))
				Expect(response.GetItems()).To(HaveLen(2))
			})

			It("Returns the total number of items", func() {
				response, err := generic.List().
					SetLimit(1).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetTotal()).To(BeNumerically("==", objectCount))
			})
		})

		Describe("Check if object exists", func() {
			It("Returns true if the object exists", func() {
				response, err := generic.Create().Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := response.GetObject()
				existsResponse, err := generic.Exists().
					SetId(object.GetId()).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(existsResponse.GetExists()).To(BeTrue())
			})

			It("Returns false if the object doesn't exist", func() {
				response, err := generic.Exists().
					SetId(uuid.New()).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetExists()).To(BeFalse())
			})
		})

		It("Updates object", func() {
			response, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						MyString: "my_value",
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()
			object.MyString = "your_value"
			updateResponse, err := generic.Update().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetMyString()).To(Equal("your_value"))
		})

		It("Returns current object when updating with no changes", func() {
			// Create an object:
			createResponse, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						MyString: "my_value",
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			created := createResponse.GetObject()
			Expect(created).ToNot(BeNil())
			Expect(created.GetId()).ToNot(BeEmpty())
			Expect(created.GetMyString()).To(Equal("my_value"))

			// Update with the same object (no changes):
			updateResponse, err := generic.Update().
				SetObject(created).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			updated := updateResponse.GetObject()
			Expect(updated).ToNot(BeNil())
			Expect(updated.GetId()).To(Equal(created.GetId()))
			Expect(updated.GetMyString()).To(Equal("my_value"))
			Expect(updated.GetMetadata()).ToNot(BeNil())
			Expect(updated.GetMetadata().GetCreationTimestamp()).To(Equal(created.GetMetadata().GetCreationTimestamp()))
		})

		It("Updates name", func() {
			// Create an object with an initial name:
			response, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						Metadata: testsv1.Metadata_builder{
							Name: "my-name",
						}.Build(),
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()
			Expect(object.GetMetadata().GetName()).To(Equal("my-name"))

			// Update the name:
			object.GetMetadata().SetName("your-name")
			updateResponse, err := generic.Update().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			Expect(object.GetMetadata().GetName()).To(Equal("your-name"))

			// Get the object and verify the name was updated:
			getResponse, err := generic.Get().
				SetId(object.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetMetadata().GetName()).To(Equal("your-name"))
		})

		It("Updates labels", func() {
			response, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						Metadata: testsv1.Metadata_builder{
							Labels: map[string]string{
								"my-label": "my-value",
							},
						}.Build(),
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()

			object.GetMetadata().SetLabels(map[string]string{
				"your-label": "your-value",
			})
			updateResponse, err := generic.Update().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			Expect(object.GetMetadata().GetLabels()).To(Equal(map[string]string{
				"your-label": "your-value",
			}))

			getResponse, err := generic.Get().
				SetId(object.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetMetadata().GetLabels()).To(Equal(map[string]string{
				"your-label": "your-value",
			}))
		})

		It("Updates annotations", func() {
			response, err := generic.Create().
				SetObject(
					testsv1.Object_builder{
						Metadata: testsv1.Metadata_builder{
							Annotations: map[string]string{
								"my-annotation": "my-value",
							},
						}.Build(),
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()

			object.GetMetadata().SetAnnotations(map[string]string{
				"your-annotation": "your-value",
			})
			updateResponse, err := generic.Update().
				SetObject(object).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			annotations := object.GetMetadata().GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("your-annotation", "your-value"))

			getResponse, err := generic.Get().
				SetId(object.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			annotations = object.GetMetadata().GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("your-annotation", "your-value"))
		})

		It("Returns not found error when updating object that doesn't exist", func() {
			_, err := generic.Update().
				SetObject(
					testsv1.Object_builder{
						Id:       "does-not-exist",
						MyString: "some-value",
					}.Build(),
				).
				Do(ctx)
			Expect(err).To(HaveOccurred())
			var notFoundErr *ErrNotFound
			Expect(errors.As(err, &notFoundErr)).To(BeTrue())
			Expect(notFoundErr.ID).To(Equal("does-not-exist"))
		})

		Describe("Filtering", func() {
			It("Filters by identifier", func() {
				for i := range 10 {
					_, err := generic.Create().
						SetObject(
							testsv1.Object_builder{
								Id: fmt.Sprintf("%d", i),
							}.Build(),
						).
						Do(ctx)
					Expect(err).ToNot(HaveOccurred())
				}
				response, err := generic.List().
					SetFilter("this.id == '5'").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("5"))
			})

			It("Filters by identifier set", func() {
				for i := range 10 {
					_, err := generic.Create().
						SetObject(
							testsv1.Object_builder{
								Id: fmt.Sprintf("%d", i),
							}.Build(),
						).
						Do(ctx)
					Expect(err).ToNot(HaveOccurred())
				}
				response, err := generic.List().
					SetFilter("this.id in ['1', '3', '5', '7', '9']").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				sort(items)
				Expect(items).To(HaveLen(5))
				Expect(items[0].GetId()).To(Equal("1"))
				Expect(items[1].GetId()).To(Equal("3"))
				Expect(items[2].GetId()).To(Equal("5"))
				Expect(items[3].GetId()).To(Equal("7"))
				Expect(items[4].GetId()).To(Equal("9"))
			})

			It("Filters by string JSON field", func() {
				for i := range 10 {
					_, err := generic.Create().
						SetObject(
							testsv1.Object_builder{
								MyString: fmt.Sprintf("my_value_%d", i),
							}.Build(),
						).
						Do(ctx)
					Expect(err).ToNot(HaveOccurred())
				}
				response, err := generic.List().
					SetFilter("this.my_string == 'my_value_5'").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetMyString()).To(Equal("my_value_5"))
			})

			It("Filters by identifier or JSON field", func() {
				for i := range 10 {
					_, err := generic.Create().
						SetObject(
							testsv1.Object_builder{
								Id:       fmt.Sprintf("%d", i),
								MyString: fmt.Sprintf("my_value_%d", i),
							}.Build(),
						).
						Do(ctx)
					Expect(err).ToNot(HaveOccurred())
				}
				response, err := generic.List().
					SetFilter("this.id == '1' || this.my_string == 'my_value_3'").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				sort(items)
				Expect(items).To(HaveLen(2))
				Expect(items[0].GetId()).To(Equal("1"))
				Expect(items[0].GetMyString()).To(Equal("my_value_1"))
				Expect(items[1].GetId()).To(Equal("3"))
				Expect(items[1].GetMyString()).To(Equal("my_value_3"))
			})

			It("Filters by identifier and JSON field", func() {
				for i := range 10 {
					_, err := generic.Create().
						SetObject(
							testsv1.Object_builder{
								Id:       fmt.Sprintf("%d", i),
								MyString: fmt.Sprintf("my_value_%d", i),
							}.Build(),
						).
						Do(ctx)
					Expect(err).ToNot(HaveOccurred())
				}
				response, err := generic.List().
					SetFilter("this.id == '1' && this.my_string == 'my_value_1'").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("1"))
				Expect(items[0].GetMyString()).To(Equal("my_value_1"))
			})

			It("Filters by calculated value", func() {
				for i := range 10 {
					_, err := generic.Create().
						SetObject(
							testsv1.Object_builder{
								MyInt32: int32(i),
							}.Build(),
						).Do(ctx)
					Expect(err).ToNot(HaveOccurred())
				}
				response, err := generic.List().
					SetFilter("(this.my_int32 + 1) == 2").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetMyInt32()).To(BeNumerically("==", 1))
			})

			It("Filters by nested JSON string field", func() {
				for i := range 10 {
					_, err := generic.Create().
						SetObject(
							testsv1.Object_builder{
								Spec: testsv1.Spec_builder{
									SpecString: fmt.Sprintf("my_value_%d", i),
								}.Build(),
							}.Build(),
						).
						Do(ctx)
					Expect(err).ToNot(HaveOccurred())
				}
				response, err := generic.List().
					SetFilter("this.spec.spec_string == 'my_value_5'").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetSpec().GetSpecString()).ToNot(BeNil())
				Expect(items[0].GetSpec().GetSpecString()).To(Equal("my_value_5"))
			})

			It("Filters deleted", func() {
				createResponse, err := generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "0",
							Metadata: testsv1.Metadata_builder{
								Finalizers: []string{"a"},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := createResponse.GetObject()
				_, err = generic.Delete().
					SetId(object.GetId()).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("this.metadata.deletion_timestamp != null").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("0"))
			})

			It("Filters not deleted", func() {
				createResponse, err := generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "0",
							Metadata: testsv1.Metadata_builder{
								Finalizers: []string{"a"},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				object := createResponse.GetObject()
				_, err = generic.Delete().
					SetId(object.GetId()).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("this.metadata.deletion_timestamp == null").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(0))
			})

			It("Filters by timestamp in the future", func() {
				var err error
				now := time.Now()
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:          "old",
							MyTimestamp: timestamppb.New(now.Add(-time.Minute)),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:          "new",
							MyTimestamp: timestamppb.New(now.Add(+time.Minute)),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("this.my_timestamp > now").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("new"))
			})

			It("Filters by timestamp in the past", func() {
				var err error
				now := time.Now()
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:          "old",
							MyTimestamp: timestamppb.New(now.Add(-time.Minute)),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:          "new",
							MyTimestamp: timestamppb.New(now.Add(+time.Minute)),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("this.my_timestamp < now").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("old"))
			})

			It("Filters by presence of message field", func() {
				var err error
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:   "good",
							Spec: testsv1.Spec_builder{}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:   "bad",
							Spec: nil,
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("has(this.spec)").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("good"))
			})

			It("Filters by presence of string field", func() {
				var err error
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:       "good",
							MyString: "my value",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:       "bad",
							MyString: "",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("has(this.my_string)").Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("good"))
			})

			It("Filters by presence of deletion timestamp", func() {
				var err error
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "good",
							Metadata: testsv1.Metadata_builder{
								Finalizers: []string{"a"},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "bad",
							Metadata: testsv1.Metadata_builder{
								Finalizers: []string{"a"},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Delete().SetId("good").Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("has(this.metadata.deletion_timestamp)").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("good"))
			})

			It("Filters by absence of deletion timestamp", func() {
				var err error
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "good",
							Metadata: testsv1.Metadata_builder{
								Finalizers: []string{"a"},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "bad",
							Metadata: testsv1.Metadata_builder{
								Finalizers: []string{"a"},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Delete().SetId("bad").Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("!has(this.metadata.deletion_timestamp)").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("good"))
			})

			It("Filters by presence of nested string field", func() {
				var err error
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "good",
							Spec: testsv1.Spec_builder{
								SpecString: "my value",
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "bad",
							Spec: testsv1.Spec_builder{
								SpecString: "",
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("has(this.spec.spec_string)").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("good"))
			})

			It("Filters by string prefix", func() {
				var err error
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:       "good",
							MyString: "my value",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:       "bad",
							MyString: "your value",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("this.my_string.startsWith('my')").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("good"))
			})

			It("Filters by string suffix", func() {
				var err error
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:       "good",
							MyString: "value my",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:       "bad",
							MyString: "value your",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("this.my_string.endsWith('my')").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("good"))
			})

			It("Escapes percent in prefix", func() {
				var err error
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:       "good",
							MyString: "my% value",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:       "bad",
							MyString: "my value",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("this.my_string.startsWith('my%')").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("good"))
			})

			It("Escapes underscore in prefix", func() {
				var err error
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:       "good",
							MyString: "my_ value",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id:       "bad",
							MyString: "my value",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				response, err := generic.List().
					SetFilter("this.my_string.startsWith('my_')").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("good"))
			})

			It("Filters by tenant", func() {
				// Create objects - they will have 'my_tenant' assigned by the tenancy logic
				_, err := generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "object_1",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "object_2",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())

				// Filter by the tenant that should be assigned by the tenancy logic
				response, err := generic.List().
					SetFilter("'my_tenant' in this.metadata.tenants").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(2)) // Both objects should have 'my_tenant'
				for _, item := range items {
					Expect(item.GetMetadata().GetTenants()).To(ContainElement("my_tenant"))
				}

				// Filter by a non-existent tenant
				response, err = generic.List().
					SetFilter("'non_existent_tenant' in this.metadata.tenants").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items = response.GetItems()
				Expect(items).To(HaveLen(0)) // No objects should match
			})

			It("Filters by label key", func() {
				_, err := generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "object_with_label",
							Metadata: testsv1.Metadata_builder{
								Labels: map[string]string{
									"mylabel": "myvalue",
								},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "object_without_label",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())

				response, err := generic.List().
					SetFilter("'mylabel' in this.metadata.labels").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("object_with_label"))
			})

			It("Filters by absence of label key", func() {
				_, err := generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "object_with_label",
							Metadata: testsv1.Metadata_builder{
								Labels: map[string]string{
									"mylabel": "myvalue",
								},
							}.Build(),
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				_, err = generic.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "object_without_label",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())

				response, err := generic.List().
					SetFilter("!('mylabel' in this.metadata.labels)").
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("object_without_label"))
			})

			It("Filters results based on tenant visibility", func() {
				// Create a new DAO with restricted tenant visibility:
				restrictedTenancyLogic := auth.NewMockTenancyLogic(ctrl)
				restrictedTenancyLogic.EXPECT().DetermineAssignableTenants(gomock.Any()).
					Return(collections.NewSet("tenant_a"), nil).
					AnyTimes()
				restrictedTenancyLogic.EXPECT().DetermineDefaultTenants(gomock.Any()).
					Return(collections.NewSet("tenant_a"), nil).
					AnyTimes()
				restrictedTenancyLogic.EXPECT().DetermineVisibleTenants(gomock.Any()).
					Return(collections.NewSet("tenant_a"), nil).
					AnyTimes()

				restrictedDAO, err := NewGenericDAO[*testsv1.Object]().
					SetLogger(logger).
					SetTable("objects").
					SetDefaultLimit(defaultLimit).
					SetMaxLimit(maxLimit).
					SetAttributionLogic(attribution).
					SetTenancyLogic(restrictedTenancyLogic).
					Build()
				Expect(err).ToNot(HaveOccurred())

				// Create objects with tenant_a (should be visible)
				_, err = restrictedDAO.Create().
					SetObject(
						testsv1.Object_builder{
							Id: "visible_object",
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())

				// Manually insert an object with tenant_b (should not be visible)
				_, err = tx.Exec(ctx, `
					insert into objects (id, tenants, data)
					values ('invisible_object', array['tenant_b'], '{}')
				`)
				Expect(err).ToNot(HaveOccurred())

				// List objects - should only return the visible one
				response, err := restrictedDAO.List().Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				items := response.GetItems()
				Expect(items).To(HaveLen(1))
				Expect(items[0].GetId()).To(Equal("visible_object"))

				// Try to get the invisible object - should return not found error
				_, err = restrictedDAO.Get().SetId("invisible_object").Do(ctx)
				Expect(err).To(HaveOccurred())
				var notFoundErr *ErrNotFound
				Expect(errors.As(err, &notFoundErr)).To(BeTrue())
				Expect(notFoundErr.ID).To(Equal("invisible_object"))

				// Check exists for invisible object - should return false
				existsResponse, err := restrictedDAO.Exists().SetId("invisible_object").Do(ctx)
				Expect(err).ToNot(HaveOccurred())
				Expect(existsResponse.GetExists()).To(BeFalse())

				// Try to delete the invisible object - should return not found error
				_, err = restrictedDAO.Delete().SetId("invisible_object").Do(ctx)
				Expect(err).To(HaveOccurred())
				Expect(errors.As(err, &notFoundErr)).To(BeTrue())
				Expect(notFoundErr.ID).To(Equal("invisible_object"))

				// Verify the object still exists in the database (using direct SQL)
				var count int
				row := tx.QueryRow(ctx, "select count(*) from objects where id = 'invisible_object'")
				err = row.Scan(&count)
				Expect(err).ToNot(HaveOccurred())
				Expect(count).To(Equal(1)) // Object should still exist
			})
		})
	})
})
