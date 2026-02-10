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
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/innabox/fulfillment-service/internal/database"
	"github.com/innabox/fulfillment-service/internal/uuid"
)

// CreateRequest represents a request to create a new object.
type CreateRequest[O Object] struct {
	request[O]
	object O
}

// SetObject sets the object to create.
func (r *CreateRequest[O]) SetObject(value O) *CreateRequest[O] {
	r.object = value
	return r
}

// Do executes the create operation and returns the response.
func (r *CreateRequest[O]) Do(ctx context.Context) (response *CreateResponse[O], err error) {
	r.tx, err = database.TxFromContext(ctx)
	if err != nil {
		return nil, err
	}
	defer r.tx.ReportError(&err)
	response, err = r.do(ctx)
	return
}

func (r *CreateRequest[O]) do(ctx context.Context) (response *CreateResponse[O], err error) {
	// Initialize the tenants:
	err = r.initTenants(ctx)
	if err != nil {
		return
	}

	// If the object is nil, create an empty one:
	if reflect.ValueOf(r.object).IsNil() {
		r.object = r.newObject()
	}

	// Generate an identifier if needed:
	id := r.object.GetId()
	if id == "" {
		id = uuid.New()
	}

	// Get the metadata:
	metadata := r.getMetadata(r.object)
	finalizers := r.getFinalizers(metadata)
	var (
		name        string
		labels      map[string]string
		annotations map[string]string
	)
	if metadata != nil {
		name = metadata.GetName()
		labels = metadata.GetLabels()
		annotations = metadata.GetAnnotations()
	}

	// Calculate the creators:
	creators, err := r.calculateCreators(ctx)
	if err != nil {
		return
	}

	// Calculate the tenants:
	tenants, err := r.calculateTenants(ctx, r.object, r.object)
	if err != nil {
		return
	}

	// Validate that tenants is not empty:
	if len(tenants) == 0 {
		err = errors.New("cannot create object with empty tenants")
		return
	}

	// Save the object:
	data, err := r.marshalData(r.object)
	if err != nil {
		return
	}
	labelsData, err := r.marshalMap(labels)
	if err != nil {
		return
	}
	annotationsData, err := r.marshalMap(annotations)
	if err != nil {
		return
	}
	sql := fmt.Sprintf(
		`
		insert into %s (
			id,
			name,
			finalizers,
			creators,
			tenants,
			labels,
			annotations,
			data
		) values (
		 	$1,
		 	$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8
		)
		returning
			creation_timestamp,
			deletion_timestamp
		`,
		r.dao.table,
	)
	row := r.tx.QueryRow(ctx, sql, id, name, finalizers, creators, tenants, labelsData, annotationsData, data)
	var (
		creationTs time.Time
		deletionTs time.Time
	)
	err = row.Scan(
		&creationTs,
		&deletionTs,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			err = &ErrAlreadyExists{
				ID: id,
			}
		}
		return
	}
	created := r.cloneObject(r.object)
	metadata = r.makeMetadata(makeMetadataArgs{
		creationTs:  creationTs,
		deletionTs:  deletionTs,
		finalizers:  finalizers,
		creators:    creators,
		tenants:     tenants,
		name:        name,
		labels:      labels,
		annotations: annotations,
	})
	created.SetId(id)
	r.setMetadata(created, metadata)

	// Fire the event:
	err = r.fireEvent(ctx, Event{
		Type:   EventTypeCreated,
		Object: created,
	})
	if err != nil {
		return
	}

	// Create the response:
	response = &CreateResponse[O]{
		object: created,
	}
	return
}

// CreateResponse represents the result of a create operation.
type CreateResponse[O Object] struct {
	object O
}

// GetObject returns the created object.
func (r *CreateResponse[O]) GetObject() O {
	return r.object
}

// Create creates and returns a new create request.
func (d *GenericDAO[O]) Create() *CreateRequest[O] {
	return &CreateRequest[O]{
		request: request[O]{
			dao: d,
		},
	}
}
