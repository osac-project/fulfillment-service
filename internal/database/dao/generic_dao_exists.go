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

	"github.com/osac-project/fulfillment-service/internal/database"
)

// ExistsRequest represents a request to check if an object exists by its identifier.
type ExistsRequest[O Object] struct {
	request[O]
	args struct {
		id string
	}
}

// SetId sets the identifier of the object to check.
func (r *ExistsRequest[O]) SetId(value string) *ExistsRequest[O] {
	r.args.id = value
	return r
}

// Do executes the exists operation and returns the response.
func (r *ExistsRequest[O]) Do(ctx context.Context) (response *ExistsResponse, err error) {
	r.tx, err = database.TxFromContext(ctx)
	if err != nil {
		return nil, err
	}
	defer r.tx.ReportError(&err)
	response, err = r.do(ctx)
	return
}

func (r *ExistsRequest[O]) do(ctx context.Context) (response *ExistsResponse, err error) {
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

	// Build the SQL statement:
	sqlBuffer := &strings.Builder{}
	fmt.Fprintf(
		sqlBuffer,
		`
		select count(*) from %s where %s
		`,
		r.dao.table,
		r.sql.filter.String(),
	)

	// Execute the SQL statement:
	sql := sqlBuffer.String()
	r.dao.logger.DebugContext(
		ctx,
		"Running SQL query",
		slog.String("sql", sql),
		slog.Any("parameters", r.sql.params),
	)
	row := r.tx.QueryRow(ctx, sql, r.sql.params...)
	var count int
	err = row.Scan(&count)
	if err != nil {
		return
	}
	response = &ExistsResponse{
		exists: count > 0,
	}
	return
}

// ExistsResponse represents the result of an exists operation.
type ExistsResponse struct {
	exists bool
}

// GetExists returns true if the object exists, false otherwise.
func (r *ExistsResponse) GetExists() bool {
	return r.exists
}

// Exists creates and returns a new exists request.
func (d *GenericDAO[O]) Exists() *ExistsRequest[O] {
	return &ExistsRequest[O]{
		request: request[O]{
			dao: d,
		},
	}
}
