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
	"fmt"

	"github.com/dustin/go-humanize/english"
)

// ErrNotFound is an error type that indicates that one or more requested objects don't exist.
type ErrNotFound struct {
	// IDs contains the identifiers of the objects that were not found.
	IDs []string
}

// Error returns the error message.
func (e *ErrNotFound) Error() string {
	switch len(e.IDs) {
	case 0:
		return "object not found"
	case 1:
		return fmt.Sprintf("object with identifier '%s' not found", e.IDs[0])
	default:
		quoted := make([]string, len(e.IDs))
		for i, id := range e.IDs {
			quoted[i] = fmt.Sprintf("'%s'", id)
		}
		return fmt.Sprintf("objects with identifiers %s not found", english.WordSeries(quoted, "and"))
	}
}

// ErrAlreadyExists is an error type that indicates that an object can't be created because it already exists.
type ErrAlreadyExists struct {
	// ID is the identifier of the object that already exists.
	ID string
}

// Error returns the error message.
func (e *ErrAlreadyExists) Error() string {
	return fmt.Sprintf("object with identifier '%s' already exists", e.ID)
}

// ErrDenied is an error type that indicates a requested operation is not allowed. The reason string is a human friendly
// description that will never contain technical details, so it can be safely returned to the user as part of the error
// response, for example as the message of a gRPC status error.
type ErrDenied struct {
	Reason string
}

// Error returns the error message.
func (e *ErrDenied) Error() string {
	return e.Reason
}
