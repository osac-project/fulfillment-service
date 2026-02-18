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
	"time"

	"github.com/osac-project/fulfillment-service/internal/database"
)

// UpdateRequest represents a request to update an existing object.
type UpdateRequest[O Object] struct {
	request[O]
	args struct {
		object O
	}
}

// SetObject sets the object to update.
func (r *UpdateRequest[O]) SetObject(value O) *UpdateRequest[O] {
	r.args.object = value
	return r
}

// Do executes the update operation and returns the response.
func (r *UpdateRequest[O]) Do(ctx context.Context) (response *UpdateResponse[O], err error) {
	r.tx, err = database.TxFromContext(ctx)
	if err != nil {
		return nil, err
	}
	defer r.tx.ReportError(&err)
	response, err = r.do(ctx)
	return
}

func (r *UpdateRequest[O]) do(ctx context.Context) (response *UpdateResponse[O], err error) {
	// Initialize the tenants:
	err = r.initTenants(ctx)
	if err != nil {
		return
	}

	// Get the current object:
	id := r.args.object.GetId()
	if id == "" {
		err = errors.New("object identifier is mandatory")
		return
	}
	current, err := r.get(ctx, id, true)
	if err != nil {
		return
	}

	// Do nothing if there are no changes:
	if r.equivalent(current, r.args.object) {
		response = &UpdateResponse[O]{
			object: current,
		}
		return
	}

	// Get the metadata:
	metadata := r.getMetadata(r.args.object)
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

	// Get the tenants:
	tenants, err := r.calculateTenants(ctx, current, r.args.object)
	if err != nil {
		return
	}

	// Save the object:
	data, err := r.marshalData(r.args.object)
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
		update %s set
			name = $1,
			finalizers = $2,
			labels = $3,
			annotations = $4,
			data = $5,
			tenants = $6
		where
			id = $7
		returning
			creation_timestamp,
			deletion_timestamp,
			creators
		`,
		r.dao.table,
	)
	row := r.tx.QueryRow(ctx, sql, name, finalizers, labelsData, annotationsData, data, tenants, id)
	var (
		creationTs time.Time
		deletionTs time.Time
		creators   []string
	)
	err = row.Scan(
		&creationTs,
		&deletionTs,
		&creators,
	)
	if err != nil {
		return
	}
	object := r.cloneObject(r.args.object)
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
	object.SetId(id)
	r.setMetadata(object, metadata)

	// Fire the event:
	err = r.fireEvent(ctx, Event{
		Type:   EventTypeUpdated,
		Object: object,
	})
	if err != nil {
		return
	}

	// If the object has been deleted and there are no finalizers we can now archive the object and fire the
	// delete event:
	if deletionTs.Unix() != 0 && len(finalizers) == 0 {
		err = r.archive(ctx, archiveArgs{
			id:              id,
			creationTs:      creationTs,
			deletionTs:      deletionTs,
			creators:        creators,
			tenants:         tenants,
			name:            name,
			labelsData:      labelsData,
			annotationsData: annotationsData,
			data:            data,
		})
		if err != nil {
			return
		}
		err = r.fireEvent(ctx, Event{
			Type:   EventTypeDeleted,
			Object: object,
		})
		if err != nil {
			return
		}
	}

	// Create and return the response:
	response = &UpdateResponse[O]{
		object: object,
	}
	return
}

// UpdateResponse represents the result of an update operation.
type UpdateResponse[O Object] struct {
	object O
}

// GetObject returns the updated object.
func (r *UpdateResponse[O]) GetObject() O {
	return r.object
}

// Update creates and returns a new update request.
func (d *GenericDAO[O]) Update() *UpdateRequest[O] {
	return &UpdateRequest[O]{
		request: request[O]{
			dao: d,
		},
	}
}
