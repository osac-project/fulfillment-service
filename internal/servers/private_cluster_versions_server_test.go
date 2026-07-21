/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

var _ = Describe("Private cluster versions server", func() {
	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivateClusterVersionsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivateClusterVersionsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivateClusterVersionsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var server *PrivateClusterVersionsServer

		// Shared test helpers:
		var (
			createCV              func(name, version string) *privatev1.ClusterVersion
			createWithState       func(name, version string, state privatev1.ClusterVersionState) *privatev1.ClusterVersion
			transitionTo          func(id string, state privatev1.ClusterVersionState)
			expectInvalidArgument func(err error, substring string)
		)

		BeforeEach(func() {
			var err error

			// Create the server:
			server, err = NewPrivateClusterVersionsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Helper to create a cluster version with default image derived from version.
			createCV = func(name, version string) *privatev1.ClusterVersion {
				response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: name}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version: version,
							Image:   "quay.io/ocp:" + version,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				return response.GetObject()
			}

			// Helper to create a cluster version in a specific state.
			createWithState = func(name, version string, state privatev1.ClusterVersionState) *privatev1.ClusterVersion {
				response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: name}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version: version,
							Image:   "quay.io/ocp:" + version,
							State:   state,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				return response.GetObject()
			}

			// Helper to transition a cluster version to a given state.
			transitionTo = func(id string, state privatev1.ClusterVersionState) {
				_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id:   id,
						Spec: privatev1.ClusterVersionSpec_builder{State: state}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.state"}},
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// Helper to assert a gRPC InvalidArgument error containing a substring.
			expectInvalidArgument = func(err error, substring string) {
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(status.Message()).To(ContainSubstring(substring))
			}
		})

		It("Creates object", func() {
			object := createCV("4-17-0", "4.17.0")
			Expect(object.GetId()).ToNot(BeEmpty())
			Expect(object.GetSpec().GetVersion()).To(Equal("4.17.0"))
			Expect(object.GetSpec().GetImage()).To(Equal("quay.io/ocp:4.17.0"))
			Expect(object.GetSpec().GetState()).To(Equal(
				privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE))
			Expect(object.GetSpec().GetEnabled()).To(BeTrue())
		})

		It("Lists objects", func() {
			const count = 10
			for i := range count {
				createCV(fmt.Sprintf("version-%d", i), fmt.Sprintf("4.17.%d", i))
			}

			response, err := server.List(ctx, privatev1.ClusterVersionsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(count))
		})

		It("Gets object", func() {
			created := createCV("4-17-0", "4.17.0")

			getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
				Id: created.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(created, getResponse.GetObject())).To(BeTrue())
		})

		It("Updates object", func() {
			object := createCV("4-17-0", "4.17.0")

			// Update enabled using a field mask:
			updateResponse, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
				Object: privatev1.ClusterVersion_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterVersionSpec_builder{
						Enabled: new(false),
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.enabled"}},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetSpec().GetEnabled()).To(BeFalse())

			// Get and verify:
			getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetSpec().GetEnabled()).To(BeFalse())
		})

		It("Deletes object", func() {
			response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
				Object: privatev1.ClusterVersion_builder{
					Metadata: privatev1.Metadata_builder{
						Name:       "4-17-0",
						Finalizers: []string{"a"},
					}.Build(),
					Spec: privatev1.ClusterVersionSpec_builder{
						Version: "4.17.0",
						Image:   "quay.io/ocp:4.17.0",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()

			_, err = server.Delete(ctx, privatev1.ClusterVersionsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})

		Describe("Field mask merge", func() {
			It("Updating state via mask preserves other spec fields", func() {
				object := createCV("merge-state", "4.17.0")
				Expect(object.GetSpec().GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE))
				Expect(object.GetSpec().GetEnabled()).To(BeTrue())

				_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id: object.GetId(),
						Spec: privatev1.ClusterVersionSpec_builder{
							State: privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED,
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.state"}},
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				updated := getResponse.GetObject()
				Expect(updated.GetSpec().GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED))
				Expect(updated.GetSpec().GetEnabled()).To(BeTrue())
				Expect(updated.GetSpec().GetVersion()).To(Equal("4.17.0"))
				Expect(updated.GetSpec().GetImage()).To(Equal("quay.io/ocp:4.17.0"))
			})

			It("Updating enabled via mask preserves other spec fields", func() {
				object := createCV("merge-enabled", "4.17.0")

				_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id: object.GetId(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Enabled: new(false),
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.enabled"}},
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				updated := getResponse.GetObject()
				Expect(updated.GetSpec().GetEnabled()).To(BeFalse())
				Expect(updated.GetSpec().GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE))
				Expect(updated.GetSpec().GetVersion()).To(Equal("4.17.0"))
				Expect(updated.GetSpec().GetImage()).To(Equal("quay.io/ocp:4.17.0"))
			})

		})

		Describe("Create defaults", func() {
			It("Defaults enabled to true when not set", func() {
				object := createCV("default-enabled", "4.17.0")
				Expect(object.GetSpec().GetEnabled()).To(BeTrue())
				Expect(object.GetSpec().HasEnabled()).To(BeTrue())
			})

			It("Respects explicit enabled=false", func() {
				response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "explicit-disabled"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version: "4.17.0",
							Image:   "quay.io/ocp:4.17.0",
							Enabled: new(false),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetObject().GetSpec().GetEnabled()).To(BeFalse())
			})

			It("Defaults state to ACTIVE when unspecified", func() {
				object := createCV("default-state", "4.17.0")
				Expect(object.GetSpec().GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE))
			})

			It("Creates with DEPRECATED state and sets deprecation timestamp", func() {
				response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "created-deprecated"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version: "4.16.0",
							Image:   "quay.io/ocp:4.16.0",
							State:   privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				spec := response.GetObject().GetSpec()
				Expect(spec.GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED))
				dep := spec.GetDeprecation()
				Expect(dep).ToNot(BeNil())
				Expect(dep.GetDeprecationTimestamp()).ToNot(BeNil())
				Expect(dep.GetDeprecationTimestamp().AsTime()).To(
					BeTemporally("~", time.Now(), time.Second))
				Expect(dep.GetObsolescenceTimestamp()).To(BeNil())
			})

			It("Creates with OBSOLETE state and sets obsolescence timestamp", func() {
				response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "created-obsolete"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version: "4.15.0",
							Image:   "quay.io/ocp:4.15.0",
							State:   privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				spec := response.GetObject().GetSpec()
				Expect(spec.GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE))
				dep := spec.GetDeprecation()
				Expect(dep).ToNot(BeNil())
				Expect(dep.GetObsolescenceTimestamp()).ToNot(BeNil())
				Expect(dep.GetObsolescenceTimestamp().AsTime()).To(
					BeTemporally("~", time.Now(), time.Second))
				Expect(dep.GetDeprecationTimestamp()).To(BeNil())
			})
		})

		Describe("SemVer validation", func() {
			DescribeTable("validates version and image",
				func(name, version, image, expectedError string) {
					response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Metadata: privatev1.Metadata_builder{Name: name}.Build(),
							Spec: privatev1.ClusterVersionSpec_builder{
								Version: version,
								Image:   image,
							}.Build(),
						}.Build(),
					}.Build())
					if expectedError != "" {
						expectInvalidArgument(err, expectedError)
					} else {
						Expect(err).ToNot(HaveOccurred())
						Expect(response.GetObject().GetSpec().GetVersion()).To(Equal(version))
					}
				},
				Entry("rejects invalid semver", "bad-semver", "not-a-version", "quay.io/ocp:latest", "SemVer"),
				Entry("rejects empty version", "empty-version", "", "quay.io/ocp:latest", "spec.version"),
				Entry("rejects empty image", "empty-image", "4.17.0", "", "spec.image"),
				Entry("accepts prerelease", "prerelease", "4.17.0-rc.1", "quay.io/ocp:4.17.0-rc.1", ""),
			)
		})

		Describe("Name auto-generation", func() {
			DescribeTable("generates name from version",
				func(inputName, version, expectedBase string) {
					cvBuilder := privatev1.ClusterVersion_builder{
						Spec: privatev1.ClusterVersionSpec_builder{
							Version: version,
							Image:   "quay.io/ocp:" + version,
						}.Build(),
					}
					if inputName != "" {
						cvBuilder.Metadata = privatev1.Metadata_builder{
							Name: inputName,
						}.Build()
					}
					response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
						Object: cvBuilder.Build(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())
					name := response.GetObject().GetMetadata().GetName()
					if inputName != "" {
						Expect(name).To(Equal(inputName))
					} else {
						Expect(name).To(HavePrefix(expectedBase + "-"))
						Expect(name).To(MatchRegexp(`-[0-9a-f]{4}$`))
						Expect(len(name)).To(BeNumerically("<=", 63))
					}
				},
				Entry("simple semver", "", "4.17.0", "4-17-0"),
				Entry("build metadata", "", "4.17.0+build.1", "4-17-0-build-1"),
				Entry("prerelease", "", "4.17.0-rc.1", "4-17-0-rc-1"),
				Entry("caller-provided name", "my-custom-name", "4.17.0", ""),
				Entry("long version truncates to 63 chars",
					"", "1.2.3-alpha.beta.gamma.delta.epsilon.zeta.eta.theta.iota.kappa.lambda",
					"1-2-3-alpha-beta-gamma-delta-epsilon-zeta-eta-theta-iota-k"),
			)
		})

		Describe("Immutability", func() {
			It("Rejects update of spec.version", func() {
				object := createCV("immutable-version", "4.17.0")

				_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id: object.GetId(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version: "4.18.0",
							Image:   "quay.io/ocp:4.17.0",
						}.Build(),
					}.Build(),
				}.Build())
				expectInvalidArgument(err, "spec.version")
			})

			It("Rejects update of spec.image", func() {
				object := createCV("immutable-image", "4.17.0")

				_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id: object.GetId(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version: "4.17.0",
							Image:   "quay.io/ocp:4.17.0-NEW",
						}.Build(),
					}.Build(),
				}.Build())
				expectInvalidArgument(err, "spec.image")
			})

			It("Rejects update of name", func() {
				object := createCV("original-name", "4.17.0")

				_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id: object.GetId(),
						Metadata: privatev1.Metadata_builder{
							Name: "renamed",
						}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version: "4.17.0",
							Image:   "quay.io/ocp:4.17.0",
						}.Build(),
					}.Build(),
				}.Build())
				expectInvalidArgument(err, "name")
			})
		})

		It("Rejects update with spec field in mask but nil spec", func() {
			object := createCV("nil-spec-guard", "4.17.0")

			_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
				Object: privatev1.ClusterVersion_builder{
					Id: object.GetId(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.state"}},
			}.Build())
			expectInvalidArgument(err, "object spec is mandatory")
		})

		It("Metadata-only update preserves spec when spec is nil in request", func() {
			object := createWithState("metadata-only", "4.17.0",
				privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED)
			originalDep := object.GetSpec().GetDeprecation()
			Expect(originalDep).ToNot(BeNil())

			// Update only metadata labels (no spec in request):
			_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
				Object: privatev1.ClusterVersion_builder{
					Id: object.GetId(),
					Metadata: privatev1.Metadata_builder{
						Labels: map[string]string{"env": "prod"},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"metadata.labels"}},
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			updated := getResponse.GetObject()
			Expect(updated.GetSpec().GetState()).To(Equal(
				privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED))
			Expect(proto.Equal(updated.GetSpec().GetDeprecation(), originalDep)).To(BeTrue())
			Expect(updated.GetMetadata().GetLabels()).To(HaveKeyWithValue("env", "prod"))
		})

		Describe("State transitions", func() {
			It("Transitions ACTIVE to DEPRECATED", func() {
				object := createWithState("active-to-deprecated", "4.17.0",
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE)

				transitionTo(object.GetId(),
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED)

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				spec := getResponse.GetObject().GetSpec()
				Expect(spec.GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED))
				dep := spec.GetDeprecation()
				Expect(dep).ToNot(BeNil())
				Expect(dep.GetDeprecationTimestamp()).ToNot(BeNil())
				Expect(dep.GetDeprecationTimestamp().AsTime()).To(
					BeTemporally("~", time.Now(), time.Second))
				Expect(dep.GetObsolescenceTimestamp()).To(BeNil())
			})

			It("Transitions ACTIVE to OBSOLETE", func() {
				object := createWithState("active-to-obsolete", "4.17.1",
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE)

				transitionTo(object.GetId(),
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE)

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				spec := getResponse.GetObject().GetSpec()
				Expect(spec.GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE))
				dep := spec.GetDeprecation()
				Expect(dep).ToNot(BeNil())
				Expect(dep.GetObsolescenceTimestamp()).ToNot(BeNil())
				Expect(dep.GetObsolescenceTimestamp().AsTime()).To(
					BeTemporally("~", time.Now(), time.Second))
				// Only target state timestamp is set:
				Expect(dep.GetDeprecationTimestamp()).To(BeNil())
			})

			It("Transitions DEPRECATED to ACTIVE", func() {
				object := createWithState("deprecated-to-active", "4.17.2",
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED)
				originalDepTimestamp := object.GetSpec().GetDeprecation().GetDeprecationTimestamp()

				transitionTo(object.GetId(),
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE)

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				spec := getResponse.GetObject().GetSpec()
				Expect(spec.GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE))
				// Backward transitions retain existing timestamps as historical record:
				dep := spec.GetDeprecation()
				Expect(dep).ToNot(BeNil())
				Expect(dep.GetDeprecationTimestamp()).ToNot(BeNil())
				Expect(proto.Equal(dep.GetDeprecationTimestamp(), originalDepTimestamp)).To(BeTrue())
			})

			It("Transitions DEPRECATED to OBSOLETE", func() {
				object := createWithState("deprecated-to-obsolete", "4.17.3",
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED)
				originalDepTimestamp := object.GetSpec().GetDeprecation().GetDeprecationTimestamp()

				transitionTo(object.GetId(),
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE)

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				spec := getResponse.GetObject().GetSpec()
				Expect(spec.GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE))
				dep := spec.GetDeprecation()
				Expect(dep).ToNot(BeNil())
				// Both timestamps should be present: deprecation retained from prior,
				// obsolescence newly set.
				Expect(dep.GetDeprecationTimestamp()).ToNot(BeNil())
				Expect(proto.Equal(dep.GetDeprecationTimestamp(), originalDepTimestamp)).To(BeTrue())
				Expect(dep.GetObsolescenceTimestamp()).ToNot(BeNil())
				Expect(dep.GetObsolescenceTimestamp().AsTime()).To(
					BeTemporally("~", time.Now(), time.Second))
			})

			It("Transitions OBSOLETE to DEPRECATED", func() {
				object := createWithState("obsolete-to-deprecated", "4.17.4",
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE)
				originalObsTimestamp := object.GetSpec().GetDeprecation().GetObsolescenceTimestamp()

				transitionTo(object.GetId(),
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED)

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				spec := getResponse.GetObject().GetSpec()
				Expect(spec.GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED))
				// Backward transitions retain existing timestamps:
				dep := spec.GetDeprecation()
				Expect(dep).ToNot(BeNil())
				Expect(dep.GetObsolescenceTimestamp()).ToNot(BeNil())
				Expect(proto.Equal(dep.GetObsolescenceTimestamp(), originalObsTimestamp)).To(BeTrue())
				// Deprecation timestamp is newly set because DEPRECATED is the target state:
				Expect(dep.GetDeprecationTimestamp()).ToNot(BeNil())
				Expect(dep.GetDeprecationTimestamp().AsTime()).To(
					BeTemporally("~", time.Now(), time.Second))
			})

			It("Transitions OBSOLETE to ACTIVE", func() {
				object := createWithState("obsolete-to-active", "4.17.5",
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE)
				originalObsTimestamp := object.GetSpec().GetDeprecation().GetObsolescenceTimestamp()

				transitionTo(object.GetId(),
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE)

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				spec := getResponse.GetObject().GetSpec()
				Expect(spec.GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE))
				// Backward transitions retain existing timestamps as historical record:
				dep := spec.GetDeprecation()
				Expect(dep).ToNot(BeNil())
				Expect(dep.GetObsolescenceTimestamp()).ToNot(BeNil())
				Expect(proto.Equal(dep.GetObsolescenceTimestamp(), originalObsTimestamp)).To(BeTrue())
			})

			It("Same-state update is no-op", func() {
				object := createWithState("same-state-noop", "4.17.6",
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED)
				originalDep := object.GetSpec().GetDeprecation()

				transitionTo(object.GetId(),
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED)

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				// Verify no timestamp changes:
				updatedDep := getResponse.GetObject().GetSpec().GetDeprecation()
				Expect(proto.Equal(updatedDep.GetDeprecationTimestamp(),
					originalDep.GetDeprecationTimestamp())).To(BeTrue())
			})

			It("Re-entry updates timestamp", func() {
				object := createWithState("re-entry-timestamp", "4.17.7",
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED)
				t1 := object.GetSpec().GetDeprecation().GetDeprecationTimestamp()

				// Transition DEPRECATED -> ACTIVE:
				transitionTo(object.GetId(),
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_ACTIVE)

				// Wait to ensure distinct timestamps:
				time.Sleep(10 * time.Millisecond)

				// Transition ACTIVE -> DEPRECATED again:
				transitionTo(object.GetId(),
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED)

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: object.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				// Verify new deprecation timestamp differs from T1:
				t2 := getResponse.GetObject().GetSpec().GetDeprecation().GetDeprecationTimestamp()
				Expect(t2).ToNot(BeNil())
				Expect(proto.Equal(t1, t2)).To(BeFalse())
			})
		})

		Describe("Default swap", func() {
			It("Create with is_default clears previous default", func() {
				// Create first version as default:
				first, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "default-first"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version:   "4.17.0",
							Image:     "quay.io/ocp:4.17.0",
							IsDefault: new(true),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(first.GetObject().GetSpec().GetIsDefault()).To(BeTrue())

				// Create second version as default:
				second, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "default-second"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version:   "4.18.0",
							Image:     "quay.io/ocp:4.18.0",
							IsDefault: new(true),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(second.GetObject().GetSpec().GetIsDefault()).To(BeTrue())

				// Verify first is no longer default:
				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: first.GetObject().GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(getResponse.GetObject().GetSpec().GetIsDefault()).To(BeFalse())
			})

			It("Update setting is_default clears previous default", func() {
				// Create first version as default:
				first, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "swap-first"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version:   "4.17.0",
							Image:     "quay.io/ocp:4.17.0",
							IsDefault: new(true),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				// Create second version (not default):
				second := createCV("swap-second", "4.18.0")

				// Update second to become default:
				_, err = server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id: second.GetId(),
						Spec: privatev1.ClusterVersionSpec_builder{
							IsDefault: new(true),
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.is_default"}},
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				// Verify second is now default:
				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: second.GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(getResponse.GetObject().GetSpec().GetIsDefault()).To(BeTrue())

				// Verify first is no longer default:
				getResponse, err = server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: first.GetObject().GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(getResponse.GetObject().GetSpec().GetIsDefault()).To(BeFalse())
			})

			It("Updating unrelated field on current default preserves is_default", func() {
				cv, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "keep-default"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version:   "4.17.0",
							Image:     "quay.io/ocp:4.17.0",
							IsDefault: new(true),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(cv.GetObject().GetSpec().GetIsDefault()).To(BeTrue())

				// Update state to DEPRECATED (is_default not in mask):
				_, err = server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id: cv.GetObject().GetId(),
						Spec: privatev1.ClusterVersionSpec_builder{
							State: privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED,
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.state"}},
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: cv.GetObject().GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(getResponse.GetObject().GetSpec().GetIsDefault()).To(BeTrue())
				Expect(getResponse.GetObject().GetSpec().GetState()).To(Equal(
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED))
			})
		})

		Describe("Auto-clear is_default", func() {
			It("Clears is_default when transitioning to OBSOLETE", func() {
				// Create as default:
				cv, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "auto-clear-obsolete"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version:   "4.17.0",
							Image:     "quay.io/ocp:4.17.0",
							IsDefault: new(true),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				// Transition to OBSOLETE:
				transitionTo(cv.GetObject().GetId(),
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE)

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: cv.GetObject().GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(getResponse.GetObject().GetSpec().GetIsDefault()).To(BeFalse())
			})

			It("Clears is_default when disabling", func() {
				// Create as default:
				cv, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "auto-clear-disabled"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version:   "4.17.0",
							Image:     "quay.io/ocp:4.17.0",
							IsDefault: new(true),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				// Disable:
				_, err = server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id: cv.GetObject().GetId(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Enabled: new(false),
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.enabled"}},
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: cv.GetObject().GetId(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(getResponse.GetObject().GetSpec().GetIsDefault()).To(BeFalse())
			})

			It("Rejects is_default on OBSOLETE update", func() {
				object := createWithState("reject-default-obsolete", "4.17.0",
					privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE)

				_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id: object.GetId(),
						Spec: privatev1.ClusterVersionSpec_builder{
							IsDefault: new(true),
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.is_default"}},
				}.Build())
				expectInvalidArgument(err, "obsolete")
			})

			It("Rejects is_default on disabled update", func() {
				response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "reject-default-disabled"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version: "4.17.0",
							Image:   "quay.io/ocp:4.17.0",
							Enabled: new(false),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				_, err = server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id: response.GetObject().GetId(),
						Spec: privatev1.ClusterVersionSpec_builder{
							IsDefault: new(true),
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.is_default"}},
				}.Build())
				expectInvalidArgument(err, "disabled")
			})

			It("Rejects is_default combined with OBSOLETE transition", func() {
				cv, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "combo-obsolete"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version:   "4.17.0",
							Image:     "quay.io/ocp:4.17.0",
							IsDefault: new(true),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				_, err = server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Id: cv.GetObject().GetId(),
						Spec: privatev1.ClusterVersionSpec_builder{
							State:     privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE,
							IsDefault: new(true),
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{
						"spec.state", "spec.is_default",
					}},
				}.Build())
				expectInvalidArgument(err, "obsolete")
			})

			It("Rejects is_default on OBSOLETE create", func() {
				_, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "create-default-obsolete"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version:   "4.17.0",
							Image:     "quay.io/ocp:4.17.0",
							State:     privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE,
							IsDefault: new(true),
						}.Build(),
					}.Build(),
				}.Build())
				expectInvalidArgument(err, "obsolete")
			})

			It("Rejects is_default on disabled create", func() {
				_, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: "create-default-disabled"}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version:   "4.17.0",
							Image:     "quay.io/ocp:4.17.0",
							Enabled:   new(false),
							IsDefault: new(true),
						}.Build(),
					}.Build(),
				}.Build())
				expectInvalidArgument(err, "disabled")
			})
		})

		It("Same-state update preserves timestamps when request omits deprecation", func() {
			object := createWithState("omitted-dep", "4.17.0",
				privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED)
			serverTimestamp := object.GetSpec().GetDeprecation().GetDeprecationTimestamp()
			Expect(serverTimestamp).ToNot(BeNil())

			// Send a same-state update with spec.deprecation in the mask but no deprecation message:
			_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
				Object: privatev1.ClusterVersion_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterVersionSpec_builder{
						State: privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED,
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{
					"spec.state", "spec.deprecation",
				}},
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			dep := getResponse.GetObject().GetSpec().GetDeprecation()
			Expect(dep).ToNot(BeNil())
			Expect(proto.Equal(dep.GetDeprecationTimestamp(), serverTimestamp)).To(BeTrue(),
				"deprecation timestamp must survive when request omits deprecation message")
		})

		It("Same-state update ignores client-supplied OUTPUT_ONLY deprecation timestamps", func() {
			// Create a DEPRECATED version (server sets deprecation_timestamp):
			object := createWithState("output-only-guard", "4.17.0",
				privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED)
			serverTimestamp := object.GetSpec().GetDeprecation().GetDeprecationTimestamp()
			Expect(serverTimestamp).ToNot(BeNil())

			// Send a same-state update with a client-supplied deprecation timestamp:
			fakeTimestamp := timestamppb.New(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
			_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
				Object: privatev1.ClusterVersion_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterVersionSpec_builder{
						State: privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_DEPRECATED,
						Deprecation: privatev1.ClusterVersionDeprecation_builder{
							DeprecationTimestamp: fakeTimestamp,
						}.Build(),
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{
					"spec.state", "spec.deprecation",
				}},
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Verify the server-set timestamp was preserved, not the client's fake:
			getResponse, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			dep := getResponse.GetObject().GetSpec().GetDeprecation()
			Expect(dep).ToNot(BeNil())
			Expect(proto.Equal(dep.GetDeprecationTimestamp(), serverTimestamp)).To(BeTrue(),
				"server-set timestamp should be preserved, not overwritten by client")
			Expect(proto.Equal(dep.GetDeprecationTimestamp(), fakeTimestamp)).To(BeFalse(),
				"client-supplied OUTPUT_ONLY timestamp should not be persisted")
		})

		Describe("Allowed upgrades", func() {
			// Helper to create a cluster version with allowed_upgrades.
			createWithAllowedUpgrades := func(name, version string,
				versionNames []string) *privatev1.ClusterVersion {
				response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
					Object: privatev1.ClusterVersion_builder{
						Metadata: privatev1.Metadata_builder{Name: name}.Build(),
						Spec: privatev1.ClusterVersionSpec_builder{
							Version: version,
							Image:   "quay.io/ocp:" + version,
							AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
								VersionNames: versionNames,
							}.Build(),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				return response.GetObject()
			}

			// Helper to get a cluster version by ID.
			getCV := func(id string) *privatev1.ClusterVersion {
				response, err := server.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
					Id: id,
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				return response.GetObject()
			}

			Describe("Create validation", func() {
				It("Creates with allowed_upgrades referencing existing active enabled versions", func() {
					target := createCV("au-target", "10.0.0")
					cv := createWithAllowedUpgrades("au-src", "10.1.0",
						[]string{target.GetMetadata().GetName()})
					Expect(cv.GetSpec().HasAllowedUpgrades()).To(BeTrue())
					Expect(cv.GetSpec().GetAllowedUpgrades().GetVersionNames()).To(
						ConsistOf(target.GetMetadata().GetName()))
				})

				It("Creates with absent allowed_upgrades", func() {
					cv := createCV("au-absent", "10.2.0")
					Expect(cv.GetSpec().HasAllowedUpgrades()).To(BeFalse())
				})

				It("Creates with present-empty allowed_upgrades", func() {
					cv := createWithAllowedUpgrades("au-empty", "10.3.0", []string{})
					Expect(cv.GetSpec().HasAllowedUpgrades()).To(BeTrue())
					Expect(cv.GetSpec().GetAllowedUpgrades().GetVersionNames()).To(BeEmpty())
				})

				It("Rejects non-existent version name", func() {
					_, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Metadata: privatev1.Metadata_builder{Name: "au-bad-ref"}.Build(),
							Spec: privatev1.ClusterVersionSpec_builder{
								Version: "10.4.0",
								Image:   "quay.io/ocp:10.4.0",
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{"no-such-version"},
								}.Build(),
							}.Build(),
						}.Build(),
					}.Build())
					expectInvalidArgument(err, "does not exist")
				})

				It("Rejects disabled version name", func() {
					response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Metadata: privatev1.Metadata_builder{Name: "au-disabled-target"}.Build(),
							Spec: privatev1.ClusterVersionSpec_builder{
								Version: "10.5.0",
								Image:   "quay.io/ocp:10.5.0",
								Enabled: new(false),
							}.Build(),
						}.Build(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					_, err = server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Metadata: privatev1.Metadata_builder{Name: "au-ref-disabled"}.Build(),
							Spec: privatev1.ClusterVersionSpec_builder{
								Version: "10.5.1",
								Image:   "quay.io/ocp:10.5.1",
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{
										response.GetObject().GetMetadata().GetName(),
									},
								}.Build(),
							}.Build(),
						}.Build(),
					}.Build())
					expectInvalidArgument(err, "disabled")
				})

				It("Rejects obsolete version name", func() {
					target := createWithState("au-obsolete-target", "10.6.0",
						privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE)

					_, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Metadata: privatev1.Metadata_builder{Name: "au-ref-obsolete"}.Build(),
							Spec: privatev1.ClusterVersionSpec_builder{
								Version: "10.6.1",
								Image:   "quay.io/ocp:10.6.1",
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{target.GetMetadata().GetName()},
								}.Build(),
							}.Build(),
						}.Build(),
					}.Build())
					expectInvalidArgument(err, "obsolete")
				})

				It("Rejects self-reference in version_names", func() {
					_, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Metadata: privatev1.Metadata_builder{Name: "au-self-ref"}.Build(),
							Spec: privatev1.ClusterVersionSpec_builder{
								Version: "10.7.0",
								Image:   "quay.io/ocp:10.7.0",
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{"au-self-ref"},
								}.Build(),
							}.Build(),
						}.Build(),
					}.Build())
					expectInvalidArgument(err, "self-reference")
				})
			})

			Describe("Update validation", func() {
				It("Updates adding valid new entries", func() {
					target := createCV("au-upd-target", "11.0.0")
					cv := createCV("au-upd-src", "11.1.0")

					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: cv.GetId(),
							Spec: privatev1.ClusterVersionSpec_builder{
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{target.GetMetadata().GetName()},
								}.Build(),
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"spec.allowed_upgrades"},
						},
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					updated := getCV(cv.GetId())
					Expect(updated.GetSpec().GetAllowedUpgrades().GetVersionNames()).To(
						ConsistOf(target.GetMetadata().GetName()))
				})

				It("Replaces list with different entries", func() {
					targetA := createCV("au-repl-a", "11.0.2")
					targetB := createCV("au-repl-b", "11.0.3")
					cv := createWithAllowedUpgrades("au-repl-src", "11.0.4",
						[]string{targetA.GetMetadata().GetName()})

					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: cv.GetId(),
							Spec: privatev1.ClusterVersionSpec_builder{
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{targetB.GetMetadata().GetName()},
								}.Build(),
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"spec.allowed_upgrades"},
						},
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					updated := getCV(cv.GetId())
					Expect(updated.GetSpec().GetAllowedUpgrades().GetVersionNames()).To(
						ConsistOf(targetB.GetMetadata().GetName()))
				})

				It("Allows retaining existing reference to now-obsolete version", func() {
					targetA := createCV("au-stale-a", "11.2.0")
					targetB := createCV("au-stale-b", "11.2.1")
					cv := createWithAllowedUpgrades("au-stale-src", "11.3.0",
						[]string{targetA.GetMetadata().GetName()})

					// Make targetA obsolete:
					transitionTo(targetA.GetId(),
						privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE)

					// Update to add targetB while keeping targetA:
					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: cv.GetId(),
							Spec: privatev1.ClusterVersionSpec_builder{
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{
										targetA.GetMetadata().GetName(),
										targetB.GetMetadata().GetName(),
									},
								}.Build(),
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"spec.allowed_upgrades"},
						},
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					updated := getCV(cv.GetId())
					Expect(updated.GetSpec().GetAllowedUpgrades().GetVersionNames()).To(
						ConsistOf(
							targetA.GetMetadata().GetName(),
							targetB.GetMetadata().GetName(),
						))
				})

				DescribeTable("Rejects adding invalid version references",
					func(slug string, setupTarget func() string, expectedError string) {
						cv := createCV("au-rej-src-"+slug, "11.4.0")
						targetName := setupTarget()

						_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
							Object: privatev1.ClusterVersion_builder{
								Id: cv.GetId(),
								Spec: privatev1.ClusterVersionSpec_builder{
									AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
										VersionNames: []string{targetName},
									}.Build(),
								}.Build(),
							}.Build(),
							UpdateMask: &fieldmaskpb.FieldMask{
								Paths: []string{"spec.allowed_upgrades"},
							},
						}.Build())
						expectInvalidArgument(err, expectedError)
					},
					Entry("non-existent version",
						"noexist",
						func() string { return "no-such-version" },
						"does not exist",
					),
					Entry("disabled version",
						"disabled",
						func() string {
							response, err := server.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
								Object: privatev1.ClusterVersion_builder{
									Metadata: privatev1.Metadata_builder{Name: "au-rej-dis-target"}.Build(),
									Spec: privatev1.ClusterVersionSpec_builder{
										Version: "11.5.0",
										Image:   "quay.io/ocp:11.5.0",
										Enabled: new(false),
									}.Build(),
								}.Build(),
							}.Build())
							Expect(err).ToNot(HaveOccurred())
							return response.GetObject().GetMetadata().GetName()
						},
						"disabled",
					),
					Entry("obsolete version",
						"obsolete",
						func() string {
							return createWithState("au-rej-obs-target", "11.6.0",
								privatev1.ClusterVersionState_CLUSTER_VERSION_STATE_OBSOLETE).GetMetadata().GetName()
						},
						"obsolete",
					),
				)

				It("Rejects self-reference on update", func() {
					cv := createCV("au-self-upd", "11.6.5")

					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: cv.GetId(),
							Spec: privatev1.ClusterVersionSpec_builder{
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{cv.GetMetadata().GetName()},
								}.Build(),
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"spec.allowed_upgrades"},
						},
					}.Build())
					expectInvalidArgument(err, "self-reference")
				})

				DescribeTable("Triggers validation with different update masks",
					func(name string, paths []string) {
						version := "11.9.0"
						cvName := "au-mask-" + name
						cv := createCV(cvName, version)

						obj := privatev1.ClusterVersion_builder{
							Id: cv.GetId(),
							Spec: privatev1.ClusterVersionSpec_builder{
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{"no-such-version"},
								}.Build(),
							}.Build(),
						}
						// Broader masks include immutable fields — supply them to pass
						// the immutability check:
						if paths == nil {
							obj.Metadata = privatev1.Metadata_builder{Name: cvName}.Build()
							obj.Spec = privatev1.ClusterVersionSpec_builder{
								Version: version,
								Image:   "quay.io/ocp:" + version,
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{"no-such-version"},
								}.Build(),
							}.Build()
						} else if len(paths) == 1 && paths[0] == "spec" {
							obj.Spec = privatev1.ClusterVersionSpec_builder{
								Version: version,
								Image:   "quay.io/ocp:" + version,
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{"no-such-version"},
								}.Build(),
							}.Build()
						}

						request := privatev1.ClusterVersionsUpdateRequest_builder{
							Object: obj.Build(),
						}
						if paths != nil {
							request.UpdateMask = &fieldmaskpb.FieldMask{Paths: paths}
						}

						_, err := server.Update(ctx, request.Build())
						expectInvalidArgument(err, "does not exist")
					},
					Entry("spec.allowed_upgrades mask",
						"au", []string{"spec.allowed_upgrades"},
					),
					Entry("parent spec mask",
						"spec", []string{"spec"},
					),
					Entry("nil mask (full replacement)",
						"nil", []string(nil),
					),
				)

				It("Updates from populated to empty list", func() {
					target := createCV("au-clear-target", "11.7.0")
					cv := createWithAllowedUpgrades("au-clear-src", "11.7.1",
						[]string{target.GetMetadata().GetName()})

					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: cv.GetId(),
							Spec: privatev1.ClusterVersionSpec_builder{
								AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
									VersionNames: []string{},
								}.Build(),
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"spec.allowed_upgrades"},
						},
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					updated := getCV(cv.GetId())
					Expect(updated.GetSpec().HasAllowedUpgrades()).To(BeTrue())
					Expect(updated.GetSpec().GetAllowedUpgrades().GetVersionNames()).To(BeEmpty())
				})

				It("Skips validation when allowed_upgrades not in update mask", func() {
					cv := createCV("au-skip-mask", "11.8.0")

					// Update only enabled — no allowed_upgrades validation should run:
					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: cv.GetId(),
							Spec: privatev1.ClusterVersionSpec_builder{
								Enabled: new(false),
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"spec.enabled"},
						},
					}.Build())
					Expect(err).ToNot(HaveOccurred())
				})
			})

			Describe("Delete cleanup", func() {
				It("Removes name from other versions' allowed_upgrades", func() {
					a := createCV("au-del-a", "12.0.0")
					b := createWithAllowedUpgrades("au-del-b", "12.0.1",
						[]string{a.GetMetadata().GetName()})

					// Add a finalizer so the object stays after soft-delete:
					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: a.GetId(),
							Metadata: privatev1.Metadata_builder{
								Finalizers: []string{"test"},
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"metadata.finalizers"},
						},
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					_, err = server.Delete(ctx, privatev1.ClusterVersionsDeleteRequest_builder{
						Id: a.GetId(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					updated := getCV(b.GetId())
					Expect(updated.GetSpec().GetAllowedUpgrades().GetVersionNames()).ToNot(
						ContainElement(a.GetMetadata().GetName()))
				})

				It("Cleans up across multiple referencing versions", func() {
					a := createCV("au-multi-a", "12.1.0")
					b := createWithAllowedUpgrades("au-multi-b", "12.1.1",
						[]string{a.GetMetadata().GetName()})
					c := createWithAllowedUpgrades("au-multi-c", "12.1.2",
						[]string{a.GetMetadata().GetName()})

					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: a.GetId(),
							Metadata: privatev1.Metadata_builder{
								Finalizers: []string{"test"},
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"metadata.finalizers"},
						},
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					_, err = server.Delete(ctx, privatev1.ClusterVersionsDeleteRequest_builder{
						Id: a.GetId(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					updatedB := getCV(b.GetId())
					Expect(updatedB.GetSpec().GetAllowedUpgrades().GetVersionNames()).ToNot(
						ContainElement(a.GetMetadata().GetName()))

					updatedC := getCV(c.GetId())
					Expect(updatedC.GetSpec().GetAllowedUpgrades().GetVersionNames()).ToNot(
						ContainElement(a.GetMetadata().GetName()))
				})

				It("Preserves allowed_upgrades message when last entry removed", func() {
					a := createCV("au-last-a", "12.2.0")
					b := createWithAllowedUpgrades("au-last-b", "12.2.1",
						[]string{a.GetMetadata().GetName()})

					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: a.GetId(),
							Metadata: privatev1.Metadata_builder{
								Finalizers: []string{"test"},
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"metadata.finalizers"},
						},
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					_, err = server.Delete(ctx, privatev1.ClusterVersionsDeleteRequest_builder{
						Id: a.GetId(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					updated := getCV(b.GetId())
					Expect(updated.GetSpec().HasAllowedUpgrades()).To(BeTrue())
					Expect(updated.GetSpec().GetAllowedUpgrades().GetVersionNames()).To(BeEmpty())
				})

				It("Increments version on affected rows", func() {
					a := createCV("au-ver-a", "12.3.0")
					b := createWithAllowedUpgrades("au-ver-b", "12.3.1",
						[]string{a.GetMetadata().GetName()})
					originalVersion := b.GetMetadata().GetVersion()

					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: a.GetId(),
							Metadata: privatev1.Metadata_builder{
								Finalizers: []string{"test"},
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"metadata.finalizers"},
						},
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					_, err = server.Delete(ctx, privatev1.ClusterVersionsDeleteRequest_builder{
						Id: a.GetId(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					updated := getCV(b.GetId())
					Expect(updated.GetMetadata().GetVersion()).To(
						BeNumerically(">", originalVersion))
				})

				It("Does not modify unreferenced versions", func() {
					a := createCV("au-unref-a", "12.4.0")
					b := createCV("au-unref-b", "12.4.1")
					originalVersion := b.GetMetadata().GetVersion()

					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: a.GetId(),
							Metadata: privatev1.Metadata_builder{
								Finalizers: []string{"test"},
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"metadata.finalizers"},
						},
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					_, err = server.Delete(ctx, privatev1.ClusterVersionsDeleteRequest_builder{
						Id: a.GetId(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					updated := getCV(b.GetId())
					Expect(updated.GetMetadata().GetVersion()).To(Equal(originalVersion))
				})

				It("Preserves non-deleted entries", func() {
					a := createCV("au-keep-a", "12.5.0")
					c := createCV("au-keep-c", "12.5.2")
					b := createWithAllowedUpgrades("au-keep-b", "12.5.1",
						[]string{
							a.GetMetadata().GetName(),
							c.GetMetadata().GetName(),
						})

					_, err := server.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
						Object: privatev1.ClusterVersion_builder{
							Id: a.GetId(),
							Metadata: privatev1.Metadata_builder{
								Finalizers: []string{"test"},
							}.Build(),
						}.Build(),
						UpdateMask: &fieldmaskpb.FieldMask{
							Paths: []string{"metadata.finalizers"},
						},
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					_, err = server.Delete(ctx, privatev1.ClusterVersionsDeleteRequest_builder{
						Id: a.GetId(),
					}.Build())
					Expect(err).ToNot(HaveOccurred())

					updated := getCV(b.GetId())
					Expect(updated.GetSpec().GetAllowedUpgrades().GetVersionNames()).To(
						ConsistOf(c.GetMetadata().GetName()))
				})
			})
		})

	})
})
