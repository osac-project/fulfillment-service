/*
Copyright (c) 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package auth

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/osac-project/fulfillment-service/internal/templating"
)

// policiesFS contains the embedded Rego policy files from the policies directory.
//
//go:embed policies/*.rego
var policiesFS embed.FS

// PolicyServerBuilder contains the data and logic needed to build an HTTP server that policy files.
type PolicyServerBuilder struct {
	logger *slog.Logger
	values map[string]any
}

// PolicyServer is an HTTP handler that serves authorization policies as Rego source code. The embedded policy
// files are processed as Go templates before being served, allowing dynamic content.
type PolicyServer struct {
	logger *slog.Logger
	values map[string]any
	engine *templating.Engine
}

// NewPolicyServer creates a builder that can then be used to configure and create a policy server.
func NewPolicyServer() *PolicyServerBuilder {
	return &PolicyServerBuilder{}
}

// SetLogger sets the logger that will be used to write to the log. This is mandatory.
func (b *PolicyServerBuilder) SetLogger(value *slog.Logger) *PolicyServerBuilder {
	b.logger = value
	return b
}

// AddValue adds a value that will be passed to the policy templates when they are executed. This is optional.
func (b *PolicyServerBuilder) AddValue(key string, value any) *PolicyServerBuilder {
	if b.values == nil {
		b.values = map[string]any{}
	}
	b.values[key] = value
	return b
}

// AddValues adds multiple values that will be passed to the policy templates when they are executed. This is optional.
func (b *PolicyServerBuilder) AddValues(values map[string]any) *PolicyServerBuilder {
	if b.values == nil {
		b.values = map[string]any{}
	}
	maps.Copy(b.values, values)
	return b
}

// SetValues sets the values that will be passed to the policy templates when they are executed. This is optional.
func (b *PolicyServerBuilder) SetValues(values map[string]any) *PolicyServerBuilder {
	b.values = maps.Clone(values)
	return b
}

// Build uses the data stored in the builder to create and configure a new policy server.
func (b *PolicyServerBuilder) Build() (result *PolicyServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Create the sub-filesystem rooted at "policies" so that file paths don't include that prefix:
	fs, err := fs.Sub(policiesFS, "policies")
	if err != nil {
		err = fmt.Errorf("failed to create filesytem for directory 'policies': %w", err)
		return
	}

	// Create the templating engine that will process the policy files:
	engine, err := templating.NewEngine().
		SetLogger(b.logger).
		AddFS(fs).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create templating engine: %w", err)
		return
	}

	// Create and populate the object:
	result = &PolicyServer{
		logger: b.logger,
		values: maps.Clone(b.values),
		engine: engine,
	}
	return
}

// ServeHTTP is the implementation of the HTTP handler interface. It serves individual policy files by name,
// processing them as Go templates before returning the result as plain text. The file name is taken from the
// request path (without the leading slash). Returns 404 if the requested file does not exist.
func (s *PolicyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	s.logger.DebugContext(
		r.Context(),
		"Serving policy",
		slog.String("method", r.Method),
		slog.String("name", name),
		slog.Any("values", s.values),
	)
	if !slices.Contains(s.engine.Names(), name) {
		http.NotFound(w, r)
		return
	}
	var buf bytes.Buffer
	err := s.engine.Execute(&buf, name, s.values)
	if err != nil {
		s.logger.ErrorContext(
			r.Context(),
			"Failed to render policy",
			slog.String("name", name),
			slog.Any("values", s.values),
			slog.Any("error", err),
		)
		http.Error(w, "Failed to render policy", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write(buf.Bytes())
}
