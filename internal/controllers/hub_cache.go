/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"google.golang.org/grpc"
	"k8s.io/client-go/tools/clientcmd"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	privatev1 "github.com/innabox/fulfillment-service/internal/api/private/v1"
)

// HubCacheProvider is the interface for accessing hub connections. The concrete
// *HubCache satisfies this interface. Consumers should depend on this interface
// to allow mocking in unit tests.
type HubCacheProvider interface {
	Get(ctx context.Context, id string) (*HubEntry, error)
}

// HubCacheBuilder contains the data and logic needed to build a cache of hub client.
type HubCacheBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
}

// HubCache caches the information and connections to the hubs.
type HubCache struct {
	logger      *slog.Logger
	client      privatev1.HubsClient
	entries     map[string]*HubEntry
	entriesLock *sync.Mutex
}

type HubEntry struct {
	Namespace string
	Client    clnt.Client
}

// NewHubCache creates a new builder that can then be used to create a new cluster order reconciler function.
func NewHubCache() *HubCacheBuilder {
	return &HubCacheBuilder{}
}

// SetLogger sets the logger. This is mandatory.
func (b *HubCacheBuilder) SetLogger(value *slog.Logger) *HubCacheBuilder {
	b.logger = value
	return b
}

// SetConnection sets the gRPC client connection. This is mandatory.
func (b *HubCacheBuilder) SetConnection(value *grpc.ClientConn) *HubCacheBuilder {
	b.connection = value
	return b
}

// Build uses the information stored in the buidler to create a new hub client cache.
func (b *HubCacheBuilder) Build() (result *HubCache, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.connection == nil {
		err = errors.New("gRPC connection is mandatory")
		return
	}

	// Create and populate the object:
	result = &HubCache{
		logger:      b.logger,
		client:      privatev1.NewHubsClient(b.connection),
		entries:     map[string]*HubEntry{},
		entriesLock: &sync.Mutex{},
	}
	return
}

func (r *HubCache) Get(ctx context.Context, id string) (result *HubEntry, err error) {
	r.entriesLock.Lock()
	defer r.entriesLock.Unlock()
	result, ok := r.entries[id]
	if ok {
		return
	}
	result, err = r.create(ctx, id)
	if err != nil {
		return
	}
	r.entries[id] = result
	return
}

func (r *HubCache) create(ctx context.Context, id string) (result *HubEntry, err error) {
	response, err := r.client.Get(ctx, privatev1.HubsGetRequest_builder{
		Id: id,
	}.Build())
	if err != nil {
		return
	}
	hub := response.GetObject()
	config, err := clientcmd.RESTConfigFromKubeConfig(hub.GetKubeconfig())
	if err != nil {
		return
	}
	client, err := clnt.New(config, clnt.Options{})
	if err != nil {
		return
	}
	result = &HubEntry{
		Namespace: hub.GetNamespace(),
		Client:    client,
	}
	return
}
