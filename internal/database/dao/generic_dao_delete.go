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
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/osac-project/fulfillment-service/internal/database"
)

// DeleteRequest represents a request to delete an object by its identifier.
type DeleteRequest[O Object] struct {
	request[O]
	args struct {
		id string
	}
}

// SetId sets the identifier of the object to delete.
func (r *DeleteRequest[O]) SetId(value string) *DeleteRequest[O] {
	r.args.id = value
	return r
}

// Do executes the delete operation and returns the response.
func (r *DeleteRequest[O]) Do(ctx context.Context) (response *DeleteResponse, err error) {
	r.tx, err = database.TxFromContext(ctx)
	if err != nil {
		return nil, err
	}
	defer r.tx.ReportError(&err)
	response, err = r.do(ctx)
	return
}

func (r *DeleteRequest[O]) do(ctx context.Context) (response *DeleteResponse, err error) {
	// Initialize the tenants:
	err = r.initTenants(ctx)
	if err != nil {
		return
	}

	// Add the id parameter:
	if r.args.id == "" {
		err = errors.New("object identifier is mandatory")
		return
	}
	r.sql.params = append(r.sql.params, r.args.id)
	r.sql.filter.WriteString("id = $1")

	// Add the tenancy filter:
	err = r.addTenancyFilter()
	if err != nil {
		return
	}

	// Set the deletion timestamp of the row and simultaneousyly retrieve the data, as we need it to fire the event
	// later:
	var buffer strings.Builder
	fmt.Fprintf(
		&buffer,
		`
		update %s set
			deletion_timestamp = now()
		where
			%s
		returning
			name,
			creation_timestamp,
			deletion_timestamp,
			finalizers,
			creators,
			tenants,
			labels,
			annotations,
			data
		`,
		r.dao.table,
		r.sql.filter.String(),
	)

	// Execute the SQL statement:
	sql := buffer.String()
	r.dao.logger.DebugContext(
		ctx,
		"Running SQL statement",
		slog.String("sql", sql),
		slog.Any("parameters", r.sql.params),
	)
	row := r.tx.QueryRow(ctx, sql, r.sql.params...)
	var (
		name            string
		creationTs      time.Time
		deletionTs      time.Time
		finalizers      []string
		creators        []string
		tenants         []string
		labelsData      []byte
		annotationsData []byte
		data            []byte
	)
	err = row.Scan(
		&name,
		&creationTs,
		&deletionTs,
		&finalizers,
		&creators,
		&tenants,
		&labelsData,
		&annotationsData,
		&data,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		err = &ErrNotFound{
			ID: r.args.id,
		}
		return
	}
	if err != nil {
		return
	}
	object := r.newObject()
	err = r.unmarshalData(data, object)
	if err != nil {
		return
	}
	labels, err := r.unmarshalMap(labelsData)
	if err != nil {
		return
	}
	annotations, err := r.unmarshalMap(annotationsData)
	if err != nil {
		return
	}
	metadata := r.makeMetadata(makeMetadataArgs{
		creationTs:  creationTs,
		deletionTs:  deletionTs,
		finalizers:  finalizers,
		creators:    creators,
		tenants:     tenants,
		name:        name,
		labels:      labels,
		annotations: annotations,
	})
	object.SetId(r.args.id)
	r.setMetadata(object, metadata)

	// If there are finalizers we need to fire the update event instead of the delete event:
	if len(finalizers) > 0 {
		err = r.fireEvent(ctx, Event{
			Type:   EventTypeUpdated,
			Object: object,
		})
		return
	}

	// If there are no finalizers we can now archive the object and fire the delete event:
	err = r.archive(ctx, archiveArgs{
		id:              r.args.id,
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

	// Create and return the response:
	response = &DeleteResponse{}
	return
}

// DeleteResponse represents the result of a delete operation.
type DeleteResponse struct {
}

// Delete creates and returns a new delete request.
func (d *GenericDAO[O]) Delete() *DeleteRequest[O] {
	return &DeleteRequest[O]{
		request: request[O]{
			dao: d,
		},
	}
}
