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
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/osac-project/fulfillment-service/internal/database"
)

// LockRequest represents a request to lock one or multiple objects.
type LockRequest[O Object] struct {
	request[O]
	args struct {
		ids []string
	}
}

// AddId adds an identifier of the object to lock.
func (r *LockRequest[O]) AddId(value string) *LockRequest[O] {
	if !slices.Contains(r.args.ids, value) {
		r.args.ids = append(r.args.ids, value)
	}
	return r
}

// AddIds adds multple identifiers of the objects to lock.
func (r *LockRequest[O]) AddIds(values ...string) *LockRequest[O] {
	for _, value := range values {
		if !slices.Contains(r.args.ids, value) {
			r.args.ids = append(r.args.ids, value)
		}
	}
	return r
}

// Do executes the lock operation and returns the response.
func (r *LockRequest[O]) Do(ctx context.Context) (response *LockResponse[O], err error) {
	r.tx, err = database.TxFromContext(ctx)
	if err != nil {
		return nil, err
	}
	defer r.tx.ReportError(&err)
	response, err = r.do(ctx)
	return
}

func (r *LockRequest[O]) do(ctx context.Context) (response *LockResponse[O], err error) {
	// Initialize the tenants:
	err = r.initTenants(ctx)
	if err != nil {
		return
	}

	// Calculate the filter:
	r.sql.params = append(r.sql.params, r.args.ids)
	fmt.Fprintf(&r.sql.filter, "id = any($%d)", len(r.sql.params))

	// Add tenant visibility filter:
	err = r.addTenancyFilter()
	if err != nil {
		return
	}

	// Prepare the SQL statement that will lock the rows. Note that the 'order by id' clause is important to avoid
	// potential deadlocks.
	var buffer strings.Builder
	fmt.Fprintf(&buffer, "select id from %s", r.dao.table)
	if r.sql.filter.Len() > 0 {
		buffer.WriteString(" where ")
		buffer.WriteString(r.sql.filter.String())
	}
	buffer.WriteString(" order by id for update")

	// Execute the SQL query:
	sql := buffer.String()
	r.dao.logger.DebugContext(
		ctx,
		"Running SQL query",
		slog.String("sql", sql),
		slog.Any("parameters", r.sql.params),
	)
	rows, err := r.tx.Query(ctx, sql, r.sql.params...)
	if err != nil {
		return
	}
	defer rows.Close()
	ids := make([]string, 0, len(r.args.ids))
	for rows.Next() {
		var id string
		err = rows.Scan(&id)
		if err != nil {
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	if err != nil {
		return
	}

	// If the identifiers returned aren't exactly the ones requested then something failed. The only reasonable
	// explanation is that some of the objects were not found, and that should be reported as a not found error.
	var notFoundIds []string
	for _, id := range r.args.ids {
		_, found := slices.BinarySearch(ids, id)
		if !found {
			notFoundIds = append(notFoundIds, id)
		}
	}
	if len(notFoundIds) > 0 {
		err = &ErrNotFound{
			IDs: notFoundIds,
		}
		return
	}

	// Create and return the response:
	response = &LockResponse[O]{}
	return
}

// LockResponse represents the result of a lock operation.
type LockResponse[O Object] struct {
}

// Lock creates and returns a new lock request.
func (d *GenericDAO[O]) Lock() *LockRequest[O] {
	return &LockRequest[O]{
		request: request[O]{
			dao: d,
		},
	}
}
