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

	"github.com/osac-project/fulfillment-service/internal/database"
)

// GetRequest represents a request to get a single object by its identifier.
type GetRequest[O Object] struct {
	request[O]
	args struct {
		id   string
		lock bool
	}
}

// SetId sets the identifier of the object to retrieve.
func (r *GetRequest[O]) SetId(value string) *GetRequest[O] {
	r.args.id = value
	return r
}

// SetLock sets whether to lock the object for update.
func (r *GetRequest[O]) SetLock(value bool) *GetRequest[O] {
	r.args.lock = value
	return r
}

// Do executes the get operation and returns the response.
func (r *GetRequest[O]) Do(ctx context.Context) (response *GetResponse[O], err error) {
	r.tx, err = database.TxFromContext(ctx)
	if err != nil {
		return nil, err
	}
	defer r.tx.ReportError(&err)
	object, err := r.get(ctx, r.args.id, r.args.lock)
	if err != nil {
		return
	}
	response = &GetResponse[O]{
		object: object,
	}
	return
}

// GetResponse represents the result of a get operation.
type GetResponse[O Object] struct {
	object O
}

// GetObject returns the retrieved object. Returns nil if the object was not found.
func (r *GetResponse[O]) GetObject() O {
	return r.object
}

// Get creates and returns a new get request.
func (d *GenericDAO[O]) Get() *GetRequest[O] {
	return &GetRequest[O]{
		request: request[O]{
			dao: d,
		},
	}
}
