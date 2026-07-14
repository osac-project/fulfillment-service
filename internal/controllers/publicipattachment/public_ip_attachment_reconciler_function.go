/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicipattachment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	osacv1alpha1 "github.com/osac-project/osac-operator/api/v1alpha1"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/annotations"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
	"github.com/osac-project/fulfillment-service/internal/masks"
)

const objectPrefix = "publicipattachment-"

// FunctionBuilder contains the data and logic needed to build a function that reconciles public IP attachments.
type FunctionBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
	hubCache   controllers.HubCache
}

type function struct {
	logger                    *slog.Logger
	hubCache                  controllers.HubCache
	publicIPAttachmentsClient privatev1.PublicIPAttachmentsClient
	publicIPsClient           privatev1.PublicIPsClient
	maskCalculator            *masks.Calculator
}

type task struct {
	r                  *function
	publicIPAttachment *privatev1.PublicIPAttachment
	hubId              string
	hubNamespace       string
	hubClient          clnt.Client
}

// NewFunction creates a new builder that can then be used to create a new public IP attachment reconciler function.
func NewFunction() *FunctionBuilder {
	return &FunctionBuilder{}
}

// SetLogger sets the logger. This is mandatory.
func (b *FunctionBuilder) SetLogger(value *slog.Logger) *FunctionBuilder {
	b.logger = value
	return b
}

// SetConnection sets the gRPC client connection. This is mandatory.
func (b *FunctionBuilder) SetConnection(value *grpc.ClientConn) *FunctionBuilder {
	b.connection = value
	return b
}

// SetHubCache sets the cache of hubs. This is mandatory.
func (b *FunctionBuilder) SetHubCache(value controllers.HubCache) *FunctionBuilder {
	b.hubCache = value
	return b
}

// Build uses the information stored in the builder to create a new public IP attachment reconciler.
func (b *FunctionBuilder) Build() (result controllers.ReconcilerFunction[*privatev1.PublicIPAttachment], err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.connection == nil {
		err = errors.New("client is mandatory")
		return
	}
	if b.hubCache == nil {
		err = errors.New("hub cache is mandatory")
		return
	}

	object := &function{
		logger:                    b.logger,
		publicIPAttachmentsClient: privatev1.NewPublicIPAttachmentsClient(b.connection),
		publicIPsClient:           privatev1.NewPublicIPsClient(b.connection),
		hubCache:                  b.hubCache,
		maskCalculator:            masks.NewCalculator().Build(),
	}
	result = object.run
	return
}

func (r *function) run(ctx context.Context, publicIPAttachment *privatev1.PublicIPAttachment) error {
	oldAttachment := proto.Clone(publicIPAttachment).(*privatev1.PublicIPAttachment)
	t := task{
		r:                  r,
		publicIPAttachment: publicIPAttachment,
	}
	var err error
	if publicIPAttachment.HasMetadata() && publicIPAttachment.GetMetadata().HasDeletionTimestamp() {
		err = t.delete(ctx)
	} else {
		err = t.update(ctx)
	}
	if err != nil {
		return err
	}
	updateMask := r.maskCalculator.Calculate(oldAttachment, publicIPAttachment)
	if len(updateMask.GetPaths()) == 0 {
		return nil
	}

	_, err = r.publicIPAttachmentsClient.Update(ctx, privatev1.PublicIPAttachmentsUpdateRequest_builder{
		Object:     publicIPAttachment,
		UpdateMask: updateMask,
	}.Build())

	return err
}

func (t *task) update(ctx context.Context) error {
	if t.addFinalizer() {
		return nil
	}

	t.setDefaults()

	if err := t.validateTenant(); err != nil {
		return err
	}

	if err := t.selectHub(ctx); err != nil {
		return err
	}

	t.publicIPAttachment.GetStatus().SetHub(t.hubId)

	object, err := t.getKubeObject(ctx)
	if err != nil {
		return err
	}

	spec := t.buildSpec()

	if object == nil {
		newObject := &osacv1alpha1.PublicIPAttachment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    t.hubNamespace,
				GenerateName: objectPrefix,
				Labels: map[string]string{
					labels.PublicIPAttachmentUuid: t.publicIPAttachment.GetId(),
				},
				Annotations: map[string]string{
					annotations.Tenant: t.publicIPAttachment.GetMetadata().GetTenant(),
				},
			},
			Spec: spec,
		}
		err = t.hubClient.Create(ctx, newObject)
		if err != nil {
			return err
		}
		t.r.logger.DebugContext(
			ctx,
			"Created public IP attachment",
			slog.String("namespace", newObject.GetNamespace()),
			slog.String("name", newObject.GetName()),
		)
	} else {
		update := object.DeepCopy()
		update.Spec = spec
		err = t.hubClient.Patch(ctx, update, clnt.MergeFrom(object))
		if err != nil {
			return err
		}
		t.r.logger.DebugContext(
			ctx,
			"Updated public IP attachment",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	}

	return err
}

func (t *task) setDefaults() {
	if !t.publicIPAttachment.HasStatus() {
		t.publicIPAttachment.SetStatus(&privatev1.PublicIPAttachmentStatus{})
	}
	if t.publicIPAttachment.GetStatus().GetState() == privatev1.PublicIPAttachmentState_PUBLIC_IP_ATTACHMENT_STATE_UNSPECIFIED {
		t.publicIPAttachment.GetStatus().SetState(privatev1.PublicIPAttachmentState_PUBLIC_IP_ATTACHMENT_STATE_PENDING)
	}
}

func (t *task) validateTenant() error {
	if !t.publicIPAttachment.HasMetadata() || t.publicIPAttachment.GetMetadata().GetTenant() == "" {
		return errors.New("public IP attachment must have a tenant assigned")
	}
	return nil
}

func (t *task) delete(ctx context.Context) (err error) {
	t.hubId = t.publicIPAttachment.GetStatus().GetHub()
	if t.hubId == "" {
		t.removeFinalizer()
		return nil
	}
	err = t.getHub(ctx)
	if err != nil {
		if errors.Is(err, controllers.ErrHubNotFound) {
			controllers.RemoveFinalizerOnDecommissionedHub(ctx, t.r.logger, t.hubId, "public_ip_attachment_id", t.publicIPAttachment.GetId(), t.removeFinalizer)
			return nil
		}
		return
	}

	object, err := t.getKubeObject(ctx)
	if err != nil {
		return
	}
	if object == nil {
		t.r.logger.DebugContext(
			ctx,
			"Public IP attachment doesn't exist",
			slog.String("id", t.publicIPAttachment.GetId()),
		)
		t.removeFinalizer()
		return
	}

	if object.GetDeletionTimestamp() == nil {
		err = t.hubClient.Delete(ctx, object)
		if err != nil {
			return
		}
		t.r.logger.DebugContext(
			ctx,
			"Deleted public IP attachment",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	} else {
		t.r.logger.DebugContext(
			ctx,
			"Public IP attachment is still being deleted, waiting for K8s finalizers",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	}

	return
}

func (t *task) selectHub(ctx context.Context) error {
	t.hubId = t.publicIPAttachment.GetStatus().GetHub()
	if t.hubId == "" {
		pipResponse, err := t.r.publicIPsClient.Get(ctx, privatev1.PublicIPsGetRequest_builder{
			Id: t.publicIPAttachment.GetSpec().GetPublicIp(),
		}.Build())
		if err != nil {
			return err
		}
		pipHub := pipResponse.GetObject().GetStatus().GetHub()
		if pipHub == "" {
			return fmt.Errorf(
				"public IP %s has no hub assigned yet, skipping",
				t.publicIPAttachment.GetSpec().GetPublicIp(),
			)
		}
		t.hubId = pipHub
	}
	t.r.logger.DebugContext(
		ctx,
		"Selected hub",
		slog.String("id", t.hubId),
	)
	hubEntry, err := t.r.hubCache.Get(ctx, t.hubId)
	if err != nil {
		return err
	}
	t.hubNamespace = hubEntry.Namespace
	t.hubClient = hubEntry.Client
	return nil
}

func (t *task) getHub(ctx context.Context) error {
	t.hubId = t.publicIPAttachment.GetStatus().GetHub()
	hubEntry, err := t.r.hubCache.Get(ctx, t.hubId)
	if err != nil {
		return err
	}
	t.hubNamespace = hubEntry.Namespace
	t.hubClient = hubEntry.Client
	return nil
}

func (t *task) getKubeObject(ctx context.Context) (result *osacv1alpha1.PublicIPAttachment, err error) {
	list := &osacv1alpha1.PublicIPAttachmentList{}
	err = t.hubClient.List(
		ctx, list,
		clnt.InNamespace(t.hubNamespace),
		clnt.MatchingLabels{
			labels.PublicIPAttachmentUuid: t.publicIPAttachment.GetId(),
		},
	)
	if err != nil {
		return
	}
	items := list.Items
	count := len(items)
	if count > 1 {
		err = fmt.Errorf(
			"expected at most one public IP attachment with identifier '%s' but found %d",
			t.publicIPAttachment.GetId(), count,
		)
		return
	}
	if count > 0 {
		result = &items[0]
	}
	return
}

func (t *task) addFinalizer() bool {
	if !t.publicIPAttachment.HasMetadata() {
		t.publicIPAttachment.SetMetadata(&privatev1.Metadata{})
	}
	list := t.publicIPAttachment.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.publicIPAttachment.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

func (t *task) removeFinalizer() {
	if !t.publicIPAttachment.HasMetadata() {
		return
	}
	list := t.publicIPAttachment.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.publicIPAttachment.GetMetadata().SetFinalizers(list)
	}
}

func (t *task) buildSpec() osacv1alpha1.PublicIPAttachmentSpec {
	spec := osacv1alpha1.PublicIPAttachmentSpec{
		PublicIP: t.publicIPAttachment.GetSpec().GetPublicIp(),
	}
	if t.publicIPAttachment.GetSpec().HasComputeInstance() {
		ci := t.publicIPAttachment.GetSpec().GetComputeInstance()
		spec.ComputeInstance = &ci
	}
	return spec
}
