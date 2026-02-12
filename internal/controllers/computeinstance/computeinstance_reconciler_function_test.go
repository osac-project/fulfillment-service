/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstance

import (
	"context"
	"errors"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	privatev1 "github.com/innabox/fulfillment-service/internal/api/private/v1"
	"github.com/innabox/fulfillment-service/internal/controllers"
	"github.com/innabox/fulfillment-service/internal/controllers/finalizers"
	"github.com/innabox/fulfillment-service/internal/kubernetes/gvks"
	"github.com/innabox/fulfillment-service/internal/kubernetes/labels"
)

var _ = Describe("buildSpec", func() {
	Describe("RestartRequestedAt field", func() {
		It("Includes restartRequestedAt in spec map when present", func() {
			requestedAt := time.Date(2026, 1, 28, 13, 27, 0, 0, time.UTC)
			cpuCores, err := anypb.New(wrapperspb.String("2"))
			Expect(err).ToNot(HaveOccurred())
			memory, err := anypb.New(wrapperspb.String("4Gi"))
			Expect(err).ToNot(HaveOccurred())
			template := "osac.templates.ocp_virt_vm"
			task := &task{
				computeInstance: privatev1.ComputeInstance_builder{
					Id: "test-instance-123",
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: template,
						TemplateParameters: map[string]*anypb.Any{
							"cpu_cores": cpuCores,
							"memory":    memory,
						},
						RestartRequestedAt: timestamppb.New(requestedAt),
					}.Build(),
				}.Build(),
			}

			// Call the actual buildSpec function
			spec, err := task.buildSpec()
			Expect(err).ToNot(HaveOccurred())

			// Verify restartRequestedAt was added with correct format
			Expect(spec["restartRequestedAt"]).To(Equal(requestedAt.Format(time.RFC3339)))

			// Verify other required fields are present
			Expect(spec["templateID"]).To(Equal(template))
			Expect(spec["templateParameters"]).ToNot(BeNil())
		})

		It("Excludes restartRequestedAt from spec map when not set", func() {
			cpuCores, err := anypb.New(wrapperspb.String("1"))
			Expect(err).ToNot(HaveOccurred())
			memory, err := anypb.New(wrapperspb.String("2Gi"))
			Expect(err).ToNot(HaveOccurred())
			template := "osac.templates.ocp_virt_vm"
			task := &task{
				computeInstance: privatev1.ComputeInstance_builder{
					Id: "test-instance-456",
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: template,
						TemplateParameters: map[string]*anypb.Any{
							"cpu_cores": cpuCores,
							"memory":    memory,
						},
						// No RestartRequestedAt set
					}.Build(),
				}.Build(),
			}

			// Call the actual buildSpec function
			spec, err := task.buildSpec()
			Expect(err).ToNot(HaveOccurred())

			// Verify restartRequestedAt was NOT added
			Expect(spec).ToNot(HaveKey("restartRequestedAt"))

			// Verify other required fields are present
			Expect(spec["templateID"]).To(Equal(template))
			Expect(spec["templateParameters"]).ToNot(BeNil())
		})
	})
})

// newComputeInstanceCR creates an unstructured ComputeInstance CR for use with the fake client.
func newComputeInstanceCR(id, namespace, name string, deletionTimestamp *metav1.Time) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvks.ComputeInstance)
	obj.SetNamespace(namespace)
	obj.SetName(name)
	obj.SetLabels(map[string]string{
		labels.ComputeInstanceUuid: id,
	})
	if deletionTimestamp != nil {
		obj.SetDeletionTimestamp(deletionTimestamp)
		obj.SetFinalizers([]string{"cloudkit.openshift.io/computeinstance"})
	}
	return obj
}

// hasFinalizer checks if the fulfillment-controller finalizer is present on the compute instance.
func hasFinalizer(ci *privatev1.ComputeInstance) bool {
	if !ci.HasMetadata() {
		return false
	}
	return slices.Contains(ci.GetMetadata().GetFinalizers(), finalizers.Controller)
}

// newTaskForDelete creates a task configured for testing delete() with hub-dependent paths.
func newTaskForDelete(ciID, hubID string, hubCache controllers.HubCache) *task {
	ci := privatev1.ComputeInstance_builder{
		Id: ciID,
		Metadata: privatev1.Metadata_builder{
			Finalizers: []string{finalizers.Controller},
		}.Build(),
		Status: privatev1.ComputeInstanceStatus_builder{
			Hub: hubID,
		}.Build(),
	}.Build()

	f := &function{
		logger:   logger,
		hubCache: hubCache,
	}

	return &task{
		r:               f,
		computeInstance: ci,
	}
}

var _ = Describe("delete", func() {
	const (
		ciID         = "test-ci-delete-id"
		hubID        = "test-hub"
		hubNamespace = "test-ns"
		crName       = "vm-test"
	)

	var (
		ctx  context.Context
		ctrl *gomock.Controller
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)
	})

	It("should remove finalizer when K8s object doesn't exist", func() {
		scheme := runtime.NewScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(&controllers.HubEntry{
				Namespace: hubNamespace,
				Client:    fakeClient,
			}, nil)

		t := newTaskForDelete(ciID, hubID, hubCache)
		Expect(hasFinalizer(t.computeInstance)).To(BeTrue())

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(hasFinalizer(t.computeInstance)).To(BeFalse())
	})

	It("should call hubClient.Delete when K8s object exists without DeletionTimestamp", func() {
		cr := newComputeInstanceCR(ciID, hubNamespace, crName, nil)

		scheme := runtime.NewScheme()
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{Group: gvks.ComputeInstance.Group, Version: gvks.ComputeInstance.Version, Kind: gvks.ComputeInstance.Kind + "List"},
			&unstructured.UnstructuredList{},
		)

		deleteCalled := false
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cr).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(ctx context.Context, client clnt.WithWatch, obj clnt.Object, opts ...clnt.DeleteOption) error {
					deleteCalled = true
					return nil
				},
			}).
			Build()

		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(&controllers.HubEntry{
				Namespace: hubNamespace,
				Client:    fakeClient,
			}, nil)

		t := newTaskForDelete(ciID, hubID, hubCache)

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(deleteCalled).To(BeTrue())
		// Finalizer should NOT be removed — K8s object still exists
		Expect(hasFinalizer(t.computeInstance)).To(BeTrue())
	})

	It("should not call hubClient.Delete when K8s object has DeletionTimestamp", func() {
		now := metav1.Now()
		cr := newComputeInstanceCR(ciID, hubNamespace, crName, &now)

		scheme := runtime.NewScheme()
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{Group: gvks.ComputeInstance.Group, Version: gvks.ComputeInstance.Version, Kind: gvks.ComputeInstance.Kind + "List"},
			&unstructured.UnstructuredList{},
		)

		deleteCalled := false
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(cr).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(ctx context.Context, client clnt.WithWatch, obj clnt.Object, opts ...clnt.DeleteOption) error {
					deleteCalled = true
					return nil
				},
			}).
			Build()

		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(&controllers.HubEntry{
				Namespace: hubNamespace,
				Client:    fakeClient,
			}, nil)

		t := newTaskForDelete(ciID, hubID, hubCache)

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(deleteCalled).To(BeFalse())
		// Finalizer should NOT be removed — K8s object still being deleted
		Expect(hasFinalizer(t.computeInstance)).To(BeTrue())
	})

	It("should propagate error when hub cache returns error", func() {
		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(nil, errors.New("hub not found"))

		t := newTaskForDelete(ciID, hubID, hubCache)

		err := t.delete(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("hub not found"))
		// Finalizer should NOT be removed on error
		Expect(hasFinalizer(t.computeInstance)).To(BeTrue())
	})

	It("should remove finalizer when no hub is assigned", func() {
		ci := privatev1.ComputeInstance_builder{
			Id: ciID,
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
			Status: privatev1.ComputeInstanceStatus_builder{
				// No hub assigned
			}.Build(),
		}.Build()

		f := &function{
			logger: logger,
		}

		t := &task{
			r:               f,
			computeInstance: ci,
		}

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(hasFinalizer(t.computeInstance)).To(BeFalse())
	})
})
