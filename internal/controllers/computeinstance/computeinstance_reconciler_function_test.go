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
	"fmt"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	osacv1alpha1 "github.com/osac-project/osac-operator/api/v1alpha1"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/gvks"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
)

var _ = Describe("buildSpec", func() {
	Describe("RestartRequestedAt field", func() {
		It("Includes restartRequestedAt in spec map when present", func() {
			ctx := context.Background()
			requestedAt := time.Date(2026, 1, 28, 13, 27, 0, 0, time.UTC)
			cpuCores, err := anypb.New(wrapperspb.String("2"))
			Expect(err).ToNot(HaveOccurred())
			memory, err := anypb.New(wrapperspb.String("4Gi"))
			Expect(err).ToNot(HaveOccurred())
			template := "osac.templates.ocp_virt_vm"

			// Set up fake client with subnet CR
			hubNamespace := "test-hub"
			subnetID := "test-subnet"
			subnetCR := &osacv1alpha1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNamespace,
					Name:      "test-sn",
					Labels:    map[string]string{labels.SubnetUuid: subnetID},
				},
			}
			scheme := runtime.NewScheme()
			Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(subnetCR).
				Build()

			task := &task{
				r: &function{logger: logger},
				computeInstance: privatev1.ComputeInstance_builder{
					Id: "test-instance-123",
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: template,
						TemplateParameters: map[string]*anypb.Any{
							"cpu_cores": cpuCores,
							"memory":    memory,
						},
						RestartRequestedAt: timestamppb.New(requestedAt),
						NetworkAttachments: []*privatev1.NetworkAttachment{
							privatev1.NetworkAttachment_builder{Subnet: subnetID}.Build(),
						},
					}.Build(),
				}.Build(),
				hubNamespace: hubNamespace,
				hubClient:    fakeClient,
			}

			// Call the actual buildSpec function
			spec, err := task.buildSpec(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify restartRequestedAt was added with correct format
			Expect(spec.RestartRequestedAt).ToNot(BeNil())
			Expect(spec.RestartRequestedAt.Time).To(Equal(requestedAt))

			// Verify other required fields are present
			Expect(spec.TemplateID).To(Equal(template))
			Expect(spec.TemplateParameters).ToNot(BeEmpty())
		})

		It("Includes explicit fields in spec map when present", func() {
			ctx := context.Background()
			template := "osac.templates.ocp_virt_vm"

			// Set up fake client with subnet CR
			hubNamespace := "test-hub"
			subnetID := "test-subnet"
			subnetCR := &osacv1alpha1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNamespace,
					Name:      "test-sn",
					Labels:    map[string]string{labels.SubnetUuid: subnetID},
				},
			}
			scheme := runtime.NewScheme()
			Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(subnetCR).
				Build()

			task := &task{
				r: &function{logger: logger},
				computeInstance: privatev1.ComputeInstance_builder{
					Id: "test-explicit-fields",
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template:    template,
						Cores:       proto.Int32(4),
						MemoryGib:   proto.Int32(8),
						RunStrategy: new("Always"),
						SshKey:      new("ssh-rsa AAAA..."),
						Image: privatev1.ComputeInstanceImage_builder{
							SourceType: "registry",
							SourceRef:  "quay.io/fedora/fedora:latest",
						}.Build(),
						BootDisk: privatev1.ComputeInstanceDisk_builder{
							SizeGib: 20,
						}.Build(),
						AdditionalDisks: []*privatev1.ComputeInstanceDisk{
							privatev1.ComputeInstanceDisk_builder{
								SizeGib: 100,
							}.Build(),
							privatev1.ComputeInstanceDisk_builder{
								SizeGib: 50,
							}.Build(),
						},
						NetworkAttachments: []*privatev1.NetworkAttachment{
							privatev1.NetworkAttachment_builder{Subnet: subnetID}.Build(),
						},
					}.Build(),
				}.Build(),
				userDataSecretName: "test-explicit-fields-user-data",
				hubNamespace:       hubNamespace,
				hubClient:          fakeClient,
			}

			spec, err := task.buildSpec(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(spec.Cores).To(Equal(int32(4)))
			Expect(spec.MemoryGiB).To(Equal(int32(8)))
			Expect(spec.RunStrategy).To(Equal(osacv1alpha1.RunStrategyType("Always")))
			Expect(spec.SSHKey).To(Equal("ssh-rsa AAAA..."))

			Expect(string(spec.Image.SourceType)).To(Equal("registry"))
			Expect(spec.Image.SourceRef).To(Equal("quay.io/fedora/fedora:latest"))

			Expect(spec.BootDisk.SizeGiB).To(Equal(int32(20)))

			Expect(spec.AdditionalDisks).To(HaveLen(2))
			Expect(spec.AdditionalDisks[0].SizeGiB).To(Equal(int32(100)))
			Expect(spec.AdditionalDisks[1].SizeGiB).To(Equal(int32(50)))

			Expect(spec.UserDataSecretRef).ToNot(BeNil())
			Expect(spec.UserDataSecretRef.Name).To(Equal("test-explicit-fields-user-data"))
		})

		It("Excludes explicit fields from spec map when not set", func() {
			ctx := context.Background()
			template := "osac.templates.ocp_virt_vm"

			// Set up fake client with subnet CR
			hubNamespace := "test-hub"
			subnetID := "test-subnet"
			subnetCR := &osacv1alpha1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNamespace,
					Name:      "test-sn",
					Labels:    map[string]string{labels.SubnetUuid: subnetID},
				},
			}
			scheme := runtime.NewScheme()
			Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(subnetCR).
				Build()

			task := &task{
				r: &function{logger: logger},
				computeInstance: privatev1.ComputeInstance_builder{
					Id: "test-no-explicit-fields",
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: template,
						NetworkAttachments: []*privatev1.NetworkAttachment{
							privatev1.NetworkAttachment_builder{Subnet: subnetID}.Build(),
						},
					}.Build(),
				}.Build(),
				hubNamespace: hubNamespace,
				hubClient:    fakeClient,
			}

			spec, err := task.buildSpec(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(spec.Cores).To(BeZero())
			Expect(spec.MemoryGiB).To(BeZero())
			Expect(spec.RunStrategy).To(BeEmpty())
			Expect(spec.SSHKey).To(BeEmpty())
			Expect(spec.Image).To(Equal(osacv1alpha1.ImageSpec{}))
			Expect(spec.BootDisk).To(Equal(osacv1alpha1.DiskSpec{}))
			Expect(spec.AdditionalDisks).To(BeEmpty())
			Expect(spec.UserDataSecretRef).To(BeNil())
		})

		It("Excludes restartRequestedAt from spec map when not set", func() {
			ctx := context.Background()
			cpuCores, err := anypb.New(wrapperspb.String("1"))
			Expect(err).ToNot(HaveOccurred())
			memory, err := anypb.New(wrapperspb.String("2Gi"))
			Expect(err).ToNot(HaveOccurred())
			template := "osac.templates.ocp_virt_vm"

			// Set up fake client with subnet CR
			hubNamespace := "test-hub"
			subnetID := "test-subnet"
			subnetCR := &osacv1alpha1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: hubNamespace,
					Name:      "test-sn",
					Labels:    map[string]string{labels.SubnetUuid: subnetID},
				},
			}
			scheme := runtime.NewScheme()
			Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(subnetCR).
				Build()

			task := &task{
				r: &function{logger: logger},
				computeInstance: privatev1.ComputeInstance_builder{
					Id: "test-instance-456",
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: template,
						TemplateParameters: map[string]*anypb.Any{
							"cpu_cores": cpuCores,
							"memory":    memory,
						},
						NetworkAttachments: []*privatev1.NetworkAttachment{
							privatev1.NetworkAttachment_builder{Subnet: subnetID}.Build(),
						},
						// No RestartRequestedAt set
					}.Build(),
				}.Build(),
				hubNamespace: hubNamespace,
				hubClient:    fakeClient,
			}

			// Call the actual buildSpec function
			spec, err := task.buildSpec(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify restartRequestedAt was NOT added
			Expect(spec.RestartRequestedAt).To(BeNil())

			// Verify other required fields are present
			Expect(spec.TemplateID).To(Equal(template))
			Expect(spec.TemplateParameters).ToNot(BeEmpty())
		})
	})
})

// newComputeInstanceCR creates a typed ComputeInstance CR for use with the fake client.
func newComputeInstanceCR(id, namespace, name string, deletionTimestamp *metav1.Time) *osacv1alpha1.ComputeInstance {
	obj := &osacv1alpha1.ComputeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				labels.ComputeInstanceUuid: id,
			},
		},
	}
	if deletionTimestamp != nil {
		obj.SetDeletionTimestamp(deletionTimestamp)
		obj.SetFinalizers([]string{"osac.openshift.io/computeinstance"})
	}
	return obj
}

// hasFinalizer checks if the fulfillment-controller finalizer is present on the compute instance.
func hasFinalizer(ci *privatev1.ComputeInstance) bool {
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
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
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
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

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
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

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

	It("should remove finalizer when hub cache returns ErrHubNotFound", func() {
		// This test verifies the core behavior: when a hub is decommissioned/deleted,
		// the reconciler removes its finalizer to allow the compute instance to be archived.
		hubCache := controllers.NewMockHubCache(ctrl)
		hubCache.EXPECT().
			Get(gomock.Any(), hubID).
			Return(nil, controllers.ErrHubNotFound)

		t := newTaskForDelete(ciID, hubID, hubCache)
		Expect(hasFinalizer(t.computeInstance)).To(BeTrue())

		err := t.delete(ctx)
		// Should return nil (not propagate the error)
		Expect(err).ToNot(HaveOccurred())
		// Finalizer should be removed to allow archiving
		Expect(hasFinalizer(t.computeInstance)).To(BeFalse())
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

var _ = Describe("getSubnetCR", func() {
	const (
		ciID         = "test-ci-subnet"
		subnetID     = "subnet-abc-123"
		hubNamespace = "test-ns"
		subnetCRName = "subnet-xyz"
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

	It("should return Subnet CR when one exists with matching label", func() {
		subnetCR := &osacv1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      subnetCRName,
				Labels: map[string]string{
					labels.SubnetUuid: subnetID,
				},
			},
		}

		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(subnetCR).
			Build()

		t := &task{
			r:            &function{logger: logger},
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		result, err := t.getSubnetCR(ctx, subnetID)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(result.GetName()).To(Equal(subnetCRName))
	})

	It("should return nil when no Subnet CR exists", func() {
		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		t := &task{
			r:            &function{logger: logger},
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		result, err := t.getSubnetCR(ctx, subnetID)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeNil())
	})

	It("should return error when multiple Subnet CRs match", func() {
		subnetCR1 := &osacv1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      "subnet-1",
				Labels: map[string]string{
					labels.SubnetUuid: subnetID,
				},
			},
		}

		subnetCR2 := &osacv1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      "subnet-2",
				Labels: map[string]string{
					labels.SubnetUuid: subnetID,
				},
			},
		}

		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(subnetCR1, subnetCR2).
			Build()

		t := &task{
			r:            &function{logger: logger},
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		result, err := t.getSubnetCR(ctx, subnetID)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("expected at most one subnet"))
		Expect(result).To(BeNil())
	})
})

var _ = Describe("getSecurityGroupCR", func() {
	const (
		hubNamespace = "test-ns"
		sgID         = "sg-abc-123"
	)

	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("should return SecurityGroup CR when one exists with matching label", func() {
		sgCR := &osacv1alpha1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      "sg-cr-name",
				Labels: map[string]string{
					labels.SecurityGroupUuid: sgID,
				},
			},
		}

		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(sgCR).
			Build()

		t := &task{
			r:            &function{logger: logger},
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		result, err := t.getSecurityGroupCR(ctx, sgID)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())
		Expect(result.GetName()).To(Equal("sg-cr-name"))
	})

	It("should return nil when no SecurityGroup CR exists", func() {
		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		t := &task{
			r:            &function{logger: logger},
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		result, err := t.getSecurityGroupCR(ctx, sgID)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeNil())
	})
})

var _ = Describe("buildSpec with subnetRef", func() {
	const (
		hubNamespace = "test-ns"
		subnetID     = "subnet-abc-123"
		subnetCRName = "subnet-xyz"
	)

	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	// Legacy subnet test cases removed - these fields are no longer supported

	It("should not set subnetRef when no subnet field", func() {
		subnetID := "test-subnet"
		subnetCR := &osacv1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      "test-sn",
				Labels:    map[string]string{labels.SubnetUuid: subnetID},
			},
		}

		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(subnetCR).
			Build()

		template := "osac.templates.ocp_virt_vm"
		t := &task{
			r: &function{logger: logger},
			computeInstance: privatev1.ComputeInstance_builder{
				Id: "test-instance",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template: template,
					NetworkAttachments: []*privatev1.NetworkAttachment{
						privatev1.NetworkAttachment_builder{Subnet: subnetID}.Build(),
					},
				}.Build(),
			}.Build(),
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		spec, err := t.buildSpec(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(spec.SubnetRef).To(BeEmpty())
	})

	It("should populate two networkAttachments and omit top-level subnetRef for multi-NIC", func() {
		sid1, sid2 := "subnet-id-1", "subnet-id-2"
		subnetCR1 := &osacv1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      "sn-1",
				Labels:    map[string]string{labels.SubnetUuid: sid1},
			},
		}

		subnetCR2 := &osacv1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      "sn-2",
				Labels:    map[string]string{labels.SubnetUuid: sid2},
			},
		}

		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(subnetCR1, subnetCR2).
			Build()

		template := "osac.templates.ocp_virt_vm"
		t := &task{
			r: &function{logger: logger},
			computeInstance: privatev1.ComputeInstance_builder{
				Id: "test-instance",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template: template,
					NetworkAttachments: []*privatev1.NetworkAttachment{
						privatev1.NetworkAttachment_builder{Subnet: sid1}.Build(),
						privatev1.NetworkAttachment_builder{Subnet: sid2}.Build(),
					},
				}.Build(),
			}.Build(),
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		spec, err := t.buildSpec(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(spec.SubnetRef).To(BeEmpty())
		Expect(spec.NetworkAttachments).To(HaveLen(2))
		Expect(spec.NetworkAttachments[0].SubnetRef).To(Equal("sn-1"))
		Expect(spec.NetworkAttachments[1].SubnetRef).To(Equal("sn-2"))
	})

	It("should resolve securityGroupRefs inside networkAttachments", func() {
		sid, sgid := "subnet-id-1", "sg-id-1"
		subnetCR := &osacv1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      "sn-1",
				Labels:    map[string]string{labels.SubnetUuid: sid},
			},
		}

		sgCR := &osacv1alpha1.SecurityGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      "sg-cr-1",
				Labels:    map[string]string{labels.SecurityGroupUuid: sgid},
			},
		}

		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(subnetCR, sgCR).
			Build()

		template := "osac.templates.ocp_virt_vm"
		t := &task{
			r: &function{logger: logger},
			computeInstance: privatev1.ComputeInstance_builder{
				Id: "test-instance",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template: template,
					NetworkAttachments: []*privatev1.NetworkAttachment{
						privatev1.NetworkAttachment_builder{
							Subnet:         sid,
							SecurityGroups: []string{sgid},
						}.Build(),
					},
				}.Build(),
			}.Build(),
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		spec, err := t.buildSpec(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(spec.NetworkAttachments).To(HaveLen(1))
		Expect(spec.NetworkAttachments[0].SubnetRef).To(Equal("sn-1"))
		Expect(spec.NetworkAttachments[0].SecurityGroupRefs).To(Equal([]string{"sg-cr-1"}))
	})

	It("should return error when hubClient.List fails for SecurityGroup lookup", func() {
		subnetCR := &osacv1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      "sn-1",
				Labels:    map[string]string{labels.SubnetUuid: "subnet-id"},
			},
		}

		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		// Create a fake client that will fail on List for SecurityGroup
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(subnetCR).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, client clnt.WithWatch, list clnt.ObjectList, opts ...clnt.ListOption) error {
					// Fail only for SecurityGroup lists
					if _, ok := list.(*osacv1alpha1.SecurityGroupList); ok {
						return fmt.Errorf("simulated List error")
					}
					return client.List(ctx, list, opts...)
				},
			}).
			Build()

		template := "osac.templates.ocp_virt_vm"
		t := &task{
			r: &function{logger: logger},
			computeInstance: privatev1.ComputeInstance_builder{
				Id: "test-instance",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template: template,
					NetworkAttachments: []*privatev1.NetworkAttachment{
						privatev1.NetworkAttachment_builder{
							Subnet:         "subnet-id",
							SecurityGroups: []string{"sg-id"},
						}.Build(),
					},
				}.Build(),
			}.Build(),
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		_, err := t.buildSpec(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to look up SecurityGroup CR"))
		Expect(err.Error()).To(ContainSubstring("simulated List error"))
	})

	It("should return error when subnet CR exists but SecurityGroup CR not found in network_attachments", func() {
		subnetCR := &osacv1alpha1.Subnet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      "sn-1",
				Labels:    map[string]string{labels.SubnetUuid: "subnet-id"},
			},
		}

		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(subnetCR).
			Build()

		template := "osac.templates.ocp_virt_vm"
		t := &task{
			r: &function{logger: logger},
			computeInstance: privatev1.ComputeInstance_builder{
				Id: "test-instance",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template: template,
					NetworkAttachments: []*privatev1.NetworkAttachment{
						privatev1.NetworkAttachment_builder{
							Subnet:         "subnet-id",
							SecurityGroups: []string{"missing-sg"},
						}.Build(),
					},
				}.Build(),
			}.Build(),
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		_, err := t.buildSpec(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("SecurityGroup CR not found"))
		Expect(err.Error()).To(ContainSubstring("missing-sg"))
	})
})

var _ = Describe("ensureUserDataSecret", func() {
	const (
		ciID         = "test-ci-user-data"
		hubNamespace = "test-ns"
		crName       = "vm-test"
		crUID        = "test-uid-123"
	)

	var (
		ctx   context.Context
		owner *osacv1alpha1.ComputeInstance
	)

	BeforeEach(func() {
		ctx = context.Background()
		owner = &osacv1alpha1.ComputeInstance{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: hubNamespace,
				Name:      crName,
				UID:       crUID,
			},
		}
	})

	It("should create a Secret with owner reference, labels, and content", func() {
		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		t := &task{
			r: &function{logger: logger},
			computeInstance: privatev1.ComputeInstance_builder{
				Id: ciID,
				Spec: privatev1.ComputeInstanceSpec_builder{
					UserData: new("#cloud-config\npackages:\n  - vim"),
				}.Build(),
			}.Build(),
			hubNamespace:       hubNamespace,
			hubClient:          fakeClient,
			userDataSecretName: ciID + userDataSecretSuffix,
		}

		err := t.ensureUserDataSecret(ctx, owner)
		Expect(err).ToNot(HaveOccurred())

		secret := &unstructured.Unstructured{}
		secret.SetGroupVersionKind(gvks.Secret)
		err = fakeClient.Get(ctx, clnt.ObjectKey{
			Namespace: hubNamespace,
			Name:      ciID + userDataSecretSuffix,
		}, secret)
		Expect(err).ToNot(HaveOccurred())

		stringData, _, _ := unstructured.NestedMap(secret.Object, "stringData")
		Expect(stringData[userDataSecretKey]).To(Equal("#cloud-config\npackages:\n  - vim"))

		Expect(secret.GetLabels()[labels.ComputeInstanceUuid]).To(Equal(ciID))

		ownerRefs := secret.GetOwnerReferences()
		Expect(ownerRefs).To(HaveLen(1))
		Expect(ownerRefs[0].Name).To(Equal(crName))
		Expect(ownerRefs[0].UID).To(Equal(owner.GetUID()))
		Expect(ownerRefs[0].Kind).To(Equal("ComputeInstance"))
	})

	It("should be idempotent when Secret already exists", func() {
		existingSecret := &unstructured.Unstructured{}
		existingSecret.SetGroupVersionKind(gvks.Secret)
		existingSecret.SetNamespace(hubNamespace)
		existingSecret.SetName(ciID + userDataSecretSuffix)

		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existingSecret).
			Build()

		t := &task{
			r: &function{logger: logger},
			computeInstance: privatev1.ComputeInstance_builder{
				Id: ciID,
				Spec: privatev1.ComputeInstanceSpec_builder{
					UserData: new("some-data"),
				}.Build(),
			}.Build(),
			hubNamespace:       hubNamespace,
			hubClient:          fakeClient,
			userDataSecretName: ciID + userDataSecretSuffix,
		}

		err := t.ensureUserDataSecret(ctx, owner)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should propagate error when Secret creation fails", func() {
		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				Create: func(ctx context.Context, client clnt.WithWatch, obj clnt.Object, opts ...clnt.CreateOption) error {
					return errors.New("create failed")
				},
			}).
			Build()

		t := &task{
			r: &function{logger: logger},
			computeInstance: privatev1.ComputeInstance_builder{
				Id: ciID,
				Spec: privatev1.ComputeInstanceSpec_builder{
					UserData: new("some-data"),
				}.Build(),
			}.Build(),
			hubNamespace:       hubNamespace,
			hubClient:          fakeClient,
			userDataSecretName: ciID + userDataSecretSuffix,
		}

		err := t.ensureUserDataSecret(ctx, owner)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("create failed"))
	})

	It("should not create a Secret when userDataSecretName is empty", func() {
		scheme := runtime.NewScheme()
		Expect(osacv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		t := &task{
			r: &function{logger: logger},
			computeInstance: privatev1.ComputeInstance_builder{
				Id:   ciID,
				Spec: privatev1.ComputeInstanceSpec_builder{}.Build(),
			}.Build(),
			hubNamespace: hubNamespace,
			hubClient:    fakeClient,
		}

		err := t.ensureUserDataSecret(ctx, owner)
		Expect(err).ToNot(HaveOccurred())
	})
})
