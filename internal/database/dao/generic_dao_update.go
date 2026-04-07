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
		lock   bool
	}
}

// SetObject sets the object to update.
func (r *UpdateRequest[O]) SetObject(value O) *UpdateRequest[O] {
	r.args.object = value
	return r
}

// SetLock sets whether to enable optimistic locking. This is optional and defaults to false.
func (r *UpdateRequest[O]) SetLock(value bool) *UpdateRequest[O] {
	r.args.lock = value
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

	// Get the requestedMetadata:
	requestedMetadata := r.getMetadata(r.args.object)
	currentMetadata := r.getMetadata(current)

	// If optimistic locking is enabled then we need to compare the version provided by the caller with the version
	// of the current object. If they are different then we need to return a conflict error.
	if r.args.lock {
		requestedVersion := requestedMetadata.GetVersion()
		currentVersion := currentMetadata.GetVersion()
		if requestedVersion != currentVersion {
			err = &ErrConflict{
				ID:               id,
				RequestedVersion: requestedVersion,
				CurrentVersion:   currentVersion,
			}
			return
		}
	}

	// Do nothing if there are no changes:
	if r.equivalent(current, r.args.object) {
		response = &UpdateResponse[O]{
			object: current,
		}
		return
	}

	// Get the requested finalizers, name, labels and annotations:
	requestedFinalizers := r.getFinalizers(requestedMetadata)
	var (
		requestedName        string
		requestedLabels      map[string]string
		requestedAnnotations map[string]string
	)
	if requestedMetadata != nil {
		requestedName = requestedMetadata.GetName()
		requestedLabels = requestedMetadata.GetLabels()
		requestedAnnotations = requestedMetadata.GetAnnotations()
	}

	// Get the requested tenants::
	requestedTenants, err := r.calculateTenants(ctx, current, r.args.object)
	if err != nil {
		return
	}

	// Marshal the requestedData, labels and annotations:
	requestedData, err := r.marshalData(r.args.object)
	if err != nil {
		return
	}
	requestedLabelsData, err := r.marshalMap(requestedLabels)
	if err != nil {
		return
	}
	requestedAnnotationsData, err := r.marshalMap(requestedAnnotations)
	if err != nil {
		return
	}

	// Prepare the SQL statement:
	sql := fmt.Sprintf(
		`
		update %s set
			name = $1,
			finalizers = $2,
			labels = $3,
			annotations = $4,
			data = $5,
			tenants = $6,
			version = version + 1
		where
			id = $7
		returning
			creation_timestamp,
			deletion_timestamp,
			creators,
			version
		`,
		r.dao.table,
	)

	// Run the SQL statement:
	var (
		updatedCreationTs time.Time
		updatedDeletionTs time.Time
		updatedCreators   []string
		updatedVersion    int32
	)
	err = func() (err error) {
		row := r.queryRow(
			ctx,
			updateOpType,
			sql,
			requestedName,
			requestedFinalizers,
			requestedLabelsData,
			requestedAnnotationsData,
			requestedData,
			requestedTenants,
			id,
		)
		start := time.Now()
		defer func() {
			r.recordOpDuration(updateOpType, start, err)
		}()
		return row.Scan(
			&updatedCreationTs,
			&updatedDeletionTs,
			&updatedCreators,
			&updatedVersion,
		)
	}()
	if err != nil {
		return
	}

	// Prepare the result:
	updatedObject := r.cloneObject(r.args.object)
	updatedMetadata := r.makeMetadata(makeMetadataArgs{
		creationTs:  updatedCreationTs,
		deletionTs:  updatedDeletionTs,
		finalizers:  requestedFinalizers,
		creators:    updatedCreators,
		tenants:     requestedTenants,
		name:        requestedName,
		labels:      requestedLabels,
		annotations: requestedAnnotations,
		version:     updatedVersion,
	})
	updatedObject.SetId(id)
	r.setMetadata(updatedObject, updatedMetadata)

	// Fire the event:
	err = r.fireEvent(ctx, Event{
		Type:   EventTypeUpdated,
		Object: updatedObject,
	})
	if err != nil {
		return
	}

	// If the object has been deleted and there are no finalizers we can now archive the object and fire the
	// delete event:
	if updatedDeletionTs.Unix() != 0 && len(requestedFinalizers) == 0 {
		err = r.archive(ctx, archiveArgs{
			id:              id,
			creationTs:      updatedCreationTs,
			deletionTs:      updatedDeletionTs,
			creators:        updatedCreators,
			tenants:         requestedTenants,
			name:            requestedName,
			labelsData:      requestedLabelsData,
			annotationsData: requestedAnnotationsData,
			version:         updatedVersion,
			data:            requestedData,
		})
		if err != nil {
			return
		}
		err = r.fireEvent(ctx, Event{
			Type:   EventTypeDeleted,
			Object: updatedObject,
		})
		if err != nil {
			return
		}
	}

	// Create and return the response:
	response = &UpdateResponse[O]{
		object: updatedObject,
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
