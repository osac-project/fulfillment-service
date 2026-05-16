/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicip

import (
	"context"
	"errors"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	osacv1alpha1 "github.com/osac-project/osac-operator/api/v1alpha1"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
)

// fakePublicIPPoolsClient implements the PublicIPPoolsClient interface for testing selectHub.
type fakePublicIPPoolsClient struct {
	privatev1.PublicIPPoolsClient
	getResponse *privatev1.PublicIPPoolsGetResponse
	getErr      error
}

func (f *fakePublicIPPoolsClient) Get(
	_ context.Context,
	_ *privatev1.PublicIPPoolsGetRequest,
	_ ...grpc.CallOption,
) (*privatev1.PublicIPPoolsGetResponse, error) {
	return f.getResponse, f.getErr
}

var _ = Describe("buildSpec", func() {
	It("Includes pool in spec", func() {
		t := &task{
			publicIP: privatev1.PublicIP_builder{
				Id: "pip-test-1",
				Spec: privatev1.PublicIPSpec_builder{
					Pool: "pool-abc123",
				}.Build(),
			}.Build(),
		}

		spec := t.buildSpec()

		Expect(spec.Pool).To(Equal("pool-abc123"))
	})

	It("Does not include status fields", func() {
		t := &task{
			publicIP: privatev1.PublicIP_builder{
				Id: "pip-test-3",
				Spec: privatev1.PublicIPSpec_builder{
					Pool: "pool-abc789",
				}.Build(),
				Status: privatev1.PublicIPStatus_builder{
					State:   privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED,
					Hub:     "hub-1",
					Address: "10.0.0.5",
				}.Build(),
			}.Build(),
		}

		spec := t.buildSpec()

		Expect(spec.Pool).To(Equal("pool-abc789"))
	})
})

// newPublicIPCR creates a typed PublicIP CR for use with the fake client.
func newPublicIPCR(id, namespace, name string, deletionTimestamp *metav1.Time) *osacv1alpha1.PublicIP {
	obj := &osacv1alpha1.PublicIP{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				labels.PublicIPUuid: id,
			},
		},
	}
	if deletionTimestamp != nil {
		obj.SetDeletionTimestamp(deletionTimestamp)
		obj.SetFinalizers([]string{"osac.openshift.io/publicip"})
	}
	return obj
}

// hasFinalizer checks if the fulfillment-controller finalizer is present on the public IP.
func hasFinalizer(publicIP *privatev1.PublicIP) bool {
	return slices.Contains(publicIP.GetMetadata().GetFinalizers(), finalizers.Controller)
}

// newTaskForDelete creates a task configured for testing delete() with hub-dependent paths.
func newTaskForDelete(publicIPID, hubID string, hubCache controllers.HubCache) *task {
	publicIP := privatev1.PublicIP_builder{
		Id: publicIPID,
		Metadata: privatev1.Metadata_builder{
			Finalizers: []string{finalizers.Controller},
		}.Build(),
		Status: privatev1.PublicIPStatus_builder{
			Hub: hubID,
		}.Build(),
	}.Build()

	f := &function{
		logger:   logger,
		hubCache: hubCache,
	}

	return &task{
		r:        f,
		publicIP: publicIP,
	}
}

var _ = Describe("delete", func() {
	const (
		publicIPID   = "pip-delete-id"
		hubID        = "test-hub"
		hubNamespace = "test-ns"
		crName       = "publicip-test"
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
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
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

		t := newTaskForDelete(publicIPID, hubID, hubCache)
		Expect(hasFinalizer(t.publicIP)).To(BeTrue())

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(hasFinalizer(t.publicIP)).To(BeFalse())
	})

	It("should call hubClient.Delete when K8s object exists without DeletionTimestamp", func() {
		cr := newPublicIPCR(publicIPID, hubNamespace, crName, nil)

		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())

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

		t := newTaskForDelete(publicIPID, hubID, hubCache)

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(deleteCalled).To(BeTrue())
		// Finalizer should NOT be removed: K8s object still exists
		Expect(hasFinalizer(t.publicIP)).To(BeTrue())
	})

	It("should not call hubClient.Delete when K8s object has DeletionTimestamp", func() {
		now := metav1.Now()
		cr := newPublicIPCR(publicIPID, hubNamespace, crName, &now)

		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())

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

		t := newTaskForDelete(publicIPID, hubID, hubCache)

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(deleteCalled).To(BeFalse())
		// Finalizer should NOT be removed: K8s object still being deleted
		Expect(hasFinalizer(t.publicIP)).To(BeTrue())
	})

	It("should propagate error when hub cache returns error", func() {
		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(nil, errors.New("hub not found"))

		t := newTaskForDelete(publicIPID, hubID, hubCache)

		err := t.delete(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("hub not found"))
		// Finalizer should NOT be removed on error
		Expect(hasFinalizer(t.publicIP)).To(BeTrue())
	})

	It("should remove finalizer when no hub is assigned", func() {
		publicIP := privatev1.PublicIP_builder{
			Id: publicIPID,
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
			Status: privatev1.PublicIPStatus_builder{
				// No hub assigned
			}.Build(),
		}.Build()

		f := &function{
			logger: logger,
		}

		t := &task{
			r:        f,
			publicIP: publicIP,
		}

		Expect(hasFinalizer(t.publicIP)).To(BeTrue())

		err := t.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(hasFinalizer(t.publicIP)).To(BeFalse())
	})
})

var _ = Describe("validateTenant", func() {
	It("should succeed when exactly one tenant is assigned", func() {
		publicIP := privatev1.PublicIP_builder{
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"tenant-1"},
			}.Build(),
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		err := t.validateTenant()
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail when no tenants are assigned", func() {
		publicIP := privatev1.PublicIP_builder{
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{},
			}.Build(),
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		err := t.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})

	It("should fail when multiple tenants are assigned", func() {
		publicIP := privatev1.PublicIP_builder{
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"tenant-1", "tenant-2"},
			}.Build(),
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		err := t.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})

	It("should fail when metadata is missing", func() {
		publicIP := privatev1.PublicIP_builder{}.Build()

		t := &task{
			publicIP: publicIP,
		}

		err := t.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})
})

var _ = Describe("setDefaults", func() {
	It("should set PENDING state when status is unspecified", func() {
		publicIP := privatev1.PublicIP_builder{
			Id: "pip-defaults",
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		t.setDefaults()

		Expect(t.publicIP.GetStatus().GetState()).To(Equal(privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING))
	})

	It("should not overwrite existing state", func() {
		publicIP := privatev1.PublicIP_builder{
			Id: "pip-existing-state",
			Status: privatev1.PublicIPStatus_builder{
				State: privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED,
			}.Build(),
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		t.setDefaults()

		Expect(t.publicIP.GetStatus().GetState()).To(Equal(privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED))
	})

	It("should create status if it doesn't exist", func() {
		publicIP := privatev1.PublicIP_builder{
			Id: "pip-no-status",
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		Expect(t.publicIP.HasStatus()).To(BeFalse())

		t.setDefaults()

		Expect(t.publicIP.HasStatus()).To(BeTrue())
		Expect(t.publicIP.GetStatus().GetState()).To(Equal(privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING))
	})
})

var _ = Describe("addFinalizer", func() {
	It("should add finalizer when not present", func() {
		publicIP := privatev1.PublicIP_builder{
			Id: "pip-no-finalizer",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{},
			}.Build(),
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		added := t.addFinalizer()

		Expect(added).To(BeTrue())
		Expect(hasFinalizer(t.publicIP)).To(BeTrue())
	})

	It("should not add finalizer when already present", func() {
		publicIP := privatev1.PublicIP_builder{
			Id: "pip-has-finalizer",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		added := t.addFinalizer()

		Expect(added).To(BeFalse())
		Expect(hasFinalizer(t.publicIP)).To(BeTrue())
		// Should not duplicate
		Expect(len(t.publicIP.GetMetadata().GetFinalizers())).To(Equal(1))
	})

	It("should create metadata if it doesn't exist", func() {
		publicIP := privatev1.PublicIP_builder{
			Id: "pip-no-metadata",
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		Expect(t.publicIP.HasMetadata()).To(BeFalse())

		added := t.addFinalizer()

		Expect(added).To(BeTrue())
		Expect(t.publicIP.HasMetadata()).To(BeTrue())
		Expect(hasFinalizer(t.publicIP)).To(BeTrue())
	})
})

var _ = Describe("removeFinalizer", func() {
	It("should remove finalizer when present", func() {
		publicIP := privatev1.PublicIP_builder{
			Id: "pip-has-finalizer",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller, "other-finalizer"},
			}.Build(),
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		Expect(hasFinalizer(t.publicIP)).To(BeTrue())

		t.removeFinalizer()

		Expect(hasFinalizer(t.publicIP)).To(BeFalse())
		// Other finalizers should remain
		Expect(t.publicIP.GetMetadata().GetFinalizers()).To(ContainElement("other-finalizer"))
	})

	It("should do nothing when finalizer not present", func() {
		publicIP := privatev1.PublicIP_builder{
			Id: "pip-no-finalizer",
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{"other-finalizer"},
			}.Build(),
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		Expect(hasFinalizer(t.publicIP)).To(BeFalse())

		t.removeFinalizer()

		Expect(hasFinalizer(t.publicIP)).To(BeFalse())
		Expect(t.publicIP.GetMetadata().GetFinalizers()).To(ContainElement("other-finalizer"))
	})

	It("should do nothing when metadata doesn't exist", func() {
		publicIP := privatev1.PublicIP_builder{
			Id: "pip-no-metadata",
		}.Build()

		t := &task{
			publicIP: publicIP,
		}

		// Should not panic
		t.removeFinalizer()

		Expect(t.publicIP.HasMetadata()).To(BeFalse())
	})
})

var _ = Describe("selectHub", func() {
	var (
		ctx  context.Context
		ctrl *gomock.Controller
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)
	})

	It("should use existing hub from status", func() {
		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), "hub-1").
			Return(&controllers.HubEntry{
				Namespace: "hub-ns",
				Client:    fake.NewClientBuilder().Build(),
			}, nil)

		publicIP := privatev1.PublicIP_builder{
			Id: "pip-existing-hub",
			Spec: privatev1.PublicIPSpec_builder{
				Pool: "pool-1",
			}.Build(),
			Status: privatev1.PublicIPStatus_builder{
				Hub: "hub-1",
			}.Build(),
		}.Build()

		f := &function{
			logger:   logger,
			hubCache: hubCache,
			// No publicIPPoolsClient needed: pool lookup should not happen
		}

		t := &task{
			r:        f,
			publicIP: publicIP,
		}

		err := t.selectHub(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(t.hubId).To(Equal("hub-1"))
		Expect(t.hubNamespace).To(Equal("hub-ns"))
	})

	It("should derive hub from pool when status hub is empty", func() {
		poolsClient := &fakePublicIPPoolsClient{
			getResponse: privatev1.PublicIPPoolsGetResponse_builder{
				Object: privatev1.PublicIPPool_builder{
					Id: "pool-1",
					Status: privatev1.PublicIPPoolStatus_builder{
						Hub: "pool-hub-1",
					}.Build(),
				}.Build(),
			}.Build(),
		}

		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), "pool-hub-1").
			Return(&controllers.HubEntry{
				Namespace: "pool-hub-ns",
				Client:    fake.NewClientBuilder().Build(),
			}, nil)

		publicIP := privatev1.PublicIP_builder{
			Id: "pip-derive-hub",
			Spec: privatev1.PublicIPSpec_builder{
				Pool: "pool-1",
			}.Build(),
		}.Build()

		f := &function{
			logger:              logger,
			hubCache:            hubCache,
			publicIPPoolsClient: poolsClient,
		}

		t := &task{
			r:        f,
			publicIP: publicIP,
		}

		err := t.selectHub(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(t.hubId).To(Equal("pool-hub-1"))
		Expect(t.hubNamespace).To(Equal("pool-hub-ns"))
	})

	It("should return error when pool hub is empty", func() {
		poolsClient := &fakePublicIPPoolsClient{
			getResponse: privatev1.PublicIPPoolsGetResponse_builder{
				Object: privatev1.PublicIPPool_builder{
					Id:     "pool-no-hub",
					Status: privatev1.PublicIPPoolStatus_builder{
						// Hub is empty: pool not yet reconciled
					}.Build(),
				}.Build(),
			}.Build(),
		}

		publicIP := privatev1.PublicIP_builder{
			Id: "pip-pool-no-hub",
			Spec: privatev1.PublicIPSpec_builder{
				Pool: "pool-no-hub",
			}.Build(),
		}.Build()

		f := &function{
			logger:              logger,
			publicIPPoolsClient: poolsClient,
		}

		t := &task{
			r:        f,
			publicIP: publicIP,
		}

		err := t.selectHub(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no hub assigned yet"))
	})

	It("should return error when pool lookup fails", func() {
		poolsClient := &fakePublicIPPoolsClient{
			getErr: errors.New("pool not found"),
		}

		publicIP := privatev1.PublicIP_builder{
			Id: "pip-pool-error",
			Spec: privatev1.PublicIPSpec_builder{
				Pool: "pool-missing",
			}.Build(),
		}.Build()

		f := &function{
			logger:              logger,
			publicIPPoolsClient: poolsClient,
		}

		t := &task{
			r:        f,
			publicIP: publicIP,
		}

		err := t.selectHub(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("pool not found"))
	})
})
