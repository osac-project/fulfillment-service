/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package console

import (
	"context"
	"fmt"
	"io"
)

// ErrSessionExists is returned when a console session already exists for a resource.
type ErrSessionExists struct {
	Resource string
	User     string
	Since    string
}

func (e *ErrSessionExists) Error() string {
	return fmt.Sprintf(
		"resource %q already has an active console session (user: %s, since: %s)",
		e.Resource, e.User, e.Since,
	)
}

// Backend provides console connections to a specific type of resource.
type Backend interface {
	// Connect establishes a console connection to the target resource and
	// returns an io.ReadWriteCloser for bidirectional communication.
	Connect(ctx context.Context, target Target) (io.ReadWriteCloser, error)
}

// Target identifies a resource to connect a console to.
type Target struct {
	ResourceType string
	ResourceID   string
	HubID        string
	Namespace    string
	VMName       string
}
