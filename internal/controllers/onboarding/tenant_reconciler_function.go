/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

// Package onboarding reconciles Tenant objects into Tenant CRDs on all hub clusters.
package onboarding

//go:generate mockgen -source=../../api/osac/private/v1/tenants_service_grpc.pb.go -destination=tenants_client_mock.go -package=onboarding TenantsClient
//go:generate mockgen -source=../../api/osac/private/v1/projects_service_grpc.pb.go -destination=projects_client_mock.go -package=onboarding ProjectsClient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	osacv1alpha1 "github.com/osac-project/osac-operator/api/v1alpha1"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
	"github.com/osac-project/fulfillment-service/internal/masks"
)

type FunctionBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
	hubCache   controllers.HubCache
}

type function struct {
	logger         *slog.Logger
	hubCache       controllers.HubCache
	tenantsClient  privatev1.TenantsClient
	projectsClient privatev1.ProjectsClient
	hubsClient     privatev1.HubsClient
	maskCalculator *masks.Calculator
}

type task struct {
	r      *function
	tenant *privatev1.Tenant
}

func NewFunction() *FunctionBuilder {
	return &FunctionBuilder{}
}

func (b *FunctionBuilder) SetLogger(value *slog.Logger) *FunctionBuilder {
	b.logger = value
	return b
}

func (b *FunctionBuilder) SetConnection(value *grpc.ClientConn) *FunctionBuilder {
	b.connection = value
	return b
}

func (b *FunctionBuilder) SetHubCache(value controllers.HubCache) *FunctionBuilder {
	b.hubCache = value
	return b
}

func (b *FunctionBuilder) Build() (result controllers.ReconcilerFunction[*privatev1.Tenant], err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.connection == nil {
		err = errors.New("connection is mandatory")
		return
	}
	if b.hubCache == nil {
		err = errors.New("hub cache is mandatory")
		return
	}

	object := &function{
		logger:         b.logger,
		tenantsClient:  privatev1.NewTenantsClient(b.connection),
		projectsClient: privatev1.NewProjectsClient(b.connection),
		hubsClient:     privatev1.NewHubsClient(b.connection),
		hubCache:       b.hubCache,
		maskCalculator: masks.NewCalculator().Build(),
	}
	result = object.run
	return
}

func (r *function) run(ctx context.Context, tenant *privatev1.Tenant) error {
	oldTenant := proto.Clone(tenant).(*privatev1.Tenant)
	t := task{
		r:      r,
		tenant: tenant,
	}
	var err error
	if tenant.HasMetadata() && tenant.GetMetadata().HasDeletionTimestamp() {
		err = t.delete(ctx)
	} else {
		err = t.update(ctx)
	}
	if err != nil {
		return err
	}
	updateMask := r.maskCalculator.Calculate(oldTenant, tenant)
	if len(updateMask.GetPaths()) > 0 {
		_, err = r.tenantsClient.Update(ctx, privatev1.TenantsUpdateRequest_builder{
			Object:     tenant,
			UpdateMask: updateMask,
		}.Build())
	}
	return err
}

func (t *task) update(ctx context.Context) error {
	if t.addFinalizer() {
		return nil
	}

	hubs, err := t.listAllHubs(ctx)
	if err != nil {
		return err
	}

	for _, hub := range hubs {
		hubEntry, err := t.r.hubCache.Get(ctx, hub.GetId())
		if err != nil {
			if errors.Is(err, controllers.ErrHubNotFound) {
				t.r.logger.DebugContext(ctx, "Hub not found, skipping",
					slog.String("hub_id", hub.GetId()),
				)
				continue
			}
			return fmt.Errorf("failed to get hub %s: %w", hub.GetId(), err)
		}

		if err := t.createOrUpdateOnHub(ctx, hub.GetId(), hubEntry); err != nil {
			return err
		}
	}

	return nil
}

func (t *task) createOrUpdateOnHub(ctx context.Context, hubId string, hubEntry *controllers.HubEntry) error {
	existing, err := t.getKubeObject(ctx, hubEntry)
	if err != nil {
		return err
	}

	tenantName := t.tenant.GetMetadata().GetName()
	if existing == nil {
		object := &osacv1alpha1.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubEntry.Namespace,
				Name:      tenantName,
				Labels: map[string]string{
					labels.TenantUuid: tenantName,
				},
			},
		}
		err = hubEntry.Client.Create(ctx, object)
		if err != nil {
			return fmt.Errorf("failed to create tenant on hub %s: %w", hubId, err)
		}
		t.r.logger.DebugContext(ctx, "Created tenant",
			slog.String("hub_id", hubId),
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	} else {
		t.r.logger.DebugContext(ctx, "Tenant already exists on hub",
			slog.String("hub_id", hubId),
			slog.String("namespace", existing.GetNamespace()),
			slog.String("name", existing.GetName()),
		)
	}

	if err := t.ensureNamespaceOnHub(ctx, hubId, hubEntry); err != nil {
		return err
	}

	return nil
}

func (t *task) ensureNamespaceOnHub(ctx context.Context, hubId string, hubEntry *controllers.HubEntry) error {
	tenantName := t.tenant.GetMetadata().GetName()
	ns := &corev1.Namespace{}
	err := hubEntry.Client.Get(ctx, clnt.ObjectKey{Name: tenantName}, ns)
	if err == nil {
		t.r.logger.DebugContext(ctx, "Tenant namespace already exists on hub",
			slog.String("hub_id", hubId),
			slog.String("name", tenantName),
		)
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get namespace on hub %s: %w", hubId, err)
	}

	ns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: tenantName,
			Labels: map[string]string{
				labels.TenantRef: tenantName,
				labels.Project:   hubEntry.Namespace,
			},
		},
	}
	err = hubEntry.Client.Create(ctx, ns)
	if err != nil {
		return fmt.Errorf("failed to create namespace on hub %s: %w", hubId, err)
	}
	t.r.logger.DebugContext(ctx, "Created tenant namespace",
		slog.String("hub_id", hubId),
		slog.String("name", tenantName),
	)
	return nil
}

func (t *task) delete(ctx context.Context) error {
	hubs, err := t.listAllHubs(ctx)
	if err != nil {
		return err
	}

	for _, hub := range hubs {
		hubEntry, err := t.r.hubCache.Get(ctx, hub.GetId())
		if err != nil {
			if errors.Is(err, controllers.ErrHubNotFound) {
				controllers.RemoveFinalizerOnDecommissionedHub(
					ctx, t.r.logger, hub.GetId(),
					"tenant_id", t.tenant.GetId(), t.removeFinalizer,
				)
				continue
			}
			return fmt.Errorf("failed to get hub %s: %w", hub.GetId(), err)
		}

		existing, err := t.getKubeObject(ctx, hubEntry)
		if err != nil {
			return err
		}
		if existing == nil {
			continue
		}

		if existing.GetDeletionTimestamp() == nil {
			err = hubEntry.Client.Delete(ctx, existing)
			if err != nil {
				return fmt.Errorf("failed to delete tenant on hub %s: %w", hub.GetId(), err)
			}
			t.r.logger.DebugContext(ctx, "Deleted tenant",
				slog.String("hub_id", hub.GetId()),
				slog.String("namespace", existing.GetNamespace()),
				slog.String("name", existing.GetName()),
			)
		} else {
			t.r.logger.DebugContext(ctx, "Tenant is still being deleted, waiting for K8s finalizers",
				slog.String("hub_id", hub.GetId()),
				slog.String("namespace", existing.GetNamespace()),
				slog.String("name", existing.GetName()),
			)
		}

		if err := t.deleteNamespaceOnHub(ctx, hub.GetId(), hubEntry); err != nil {
			return err
		}

		return nil
	}

	// Wait for all projects to be archived before removing the finalizer —
	// otherwise the DAO archive hits FK violations from the projects table.
	listFilter := fmt.Sprintf("this.metadata.tenant == %q", t.tenant.GetMetadata().GetName())
	listResp, err := t.r.projectsClient.List(ctx, privatev1.ProjectsListRequest_builder{
		Filter: new(listFilter),
		Limit:  new(int32(0)),
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to query for remaining projects: %w", err)
	}
	if listResp.GetTotal() > 0 {
		return fmt.Errorf("tenant still has %d project(s) pending deletion", listResp.GetTotal())
	}

	t.removeFinalizer()
	return nil
}

func (t *task) deleteNamespaceOnHub(ctx context.Context, hubId string, hubEntry *controllers.HubEntry) error {
	tenantName := t.tenant.GetMetadata().GetName()
	ns := &corev1.Namespace{}
	err := hubEntry.Client.Get(ctx, clnt.ObjectKey{Name: tenantName}, ns)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get namespace on hub %s: %w", hubId, err)
	}
	if ns.GetDeletionTimestamp() != nil {
		t.r.logger.DebugContext(ctx, "Tenant namespace is already being deleted",
			slog.String("hub_id", hubId),
			slog.String("name", tenantName),
		)
		return nil
	}
	err = hubEntry.Client.Delete(ctx, ns)
	if err != nil {
		return fmt.Errorf("failed to delete namespace on hub %s: %w", hubId, err)
	}
	t.r.logger.DebugContext(ctx, "Deleted tenant namespace",
		slog.String("hub_id", hubId),
		slog.String("name", tenantName),
	)
	return nil
}

func (t *task) getKubeObject(ctx context.Context, hubEntry *controllers.HubEntry) (result *osacv1alpha1.Tenant, err error) {
	tenantName := t.tenant.GetMetadata().GetName()
	object := &osacv1alpha1.Tenant{}
	err = hubEntry.Client.Get(ctx, clnt.ObjectKey{
		Namespace: hubEntry.Namespace,
		Name:      tenantName,
	}, object)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = nil
			return
		}
		return
	}
	result = object
	return
}

func (t *task) listAllHubs(ctx context.Context) ([]*privatev1.Hub, error) {
	var allHubs []*privatev1.Hub
	var offset int32
	for {
		response, err := t.r.hubsClient.List(ctx, privatev1.HubsListRequest_builder{
			Offset: &offset,
		}.Build())
		if err != nil {
			return nil, fmt.Errorf("failed to list hubs: %w", err)
		}
		allHubs = append(allHubs, response.GetItems()...)
		total := response.GetTotal()
		if total <= 0 || offset+response.GetSize() >= total {
			break
		}
		offset += response.GetSize()
	}
	return allHubs, nil
}

func (t *task) addFinalizer() bool {
	if !t.tenant.HasMetadata() {
		t.tenant.SetMetadata(&privatev1.Metadata{})
	}
	list := t.tenant.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.tenant.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

func (t *task) removeFinalizer() {
	if !t.tenant.HasMetadata() {
		return
	}
	list := t.tenant.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.tenant.GetMetadata().SetFinalizers(list)
	}
}
