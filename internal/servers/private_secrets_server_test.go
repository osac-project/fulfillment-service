/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/events"
)

var _ = Describe("Private secrets server", func() {
	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivateSecretsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivateSecretsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivateSecretsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var server *PrivateSecretsServer

		BeforeEach(func() {
			var err error
			server, err = NewPrivateSecretsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		createVaultSecret := func() *privatev1.Secret {
			response, err := server.Create(ctx, privatev1.SecretsCreateRequest_builder{
				Object: privatev1.Secret_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "my-secret",
					}.Build(),
					Spec: privatev1.SecretSpec_builder{
						Data: map[string][]byte{
							"key": []byte("value"),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			return response.GetObject()
		}

		createVaultSecretWithName := func(name string) *privatev1.Secret {
			response, err := server.Create(ctx, privatev1.SecretsCreateRequest_builder{
				Object: privatev1.Secret_builder{
					Metadata: privatev1.Metadata_builder{
						Name: name,
					}.Build(),
					Spec: privatev1.SecretSpec_builder{
						Data: map[string][]byte{
							"key": []byte("value"),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			return response.GetObject()
		}

		createHubSecret := func() *privatev1.Secret {
			response, err := server.Create(ctx, privatev1.SecretsCreateRequest_builder{
				Object: privatev1.Secret_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "hub-secret",
					}.Build(),
					Spec: privatev1.SecretSpec_builder{
						Backend: privatev1.SecretBackend_SECRET_BACKEND_HUB,
						Coordinates: map[string]string{
							"cluster":   "hub-1",
							"namespace": "default",
							"name":      "my-k8s-secret",
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			return response.GetObject()
		}

		It("Creates and gets a Vault secret", func() {
			created := createVaultSecret()

			Expect(created.GetId()).ToNot(BeEmpty())
			Expect(created.GetSpec().GetBackend()).To(Equal(
				privatev1.SecretBackend_SECRET_BACKEND_VAULT))
			Expect(created.GetSpec().GetData()).To(HaveKeyWithValue("key", []byte("value")))

			getResponse, err := server.Get(ctx, privatev1.SecretsGetRequest_builder{
				Id: created.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			obj := getResponse.GetObject()
			Expect(obj.GetId()).To(Equal(created.GetId()))
			Expect(obj.GetSpec().GetBackend()).To(Equal(
				privatev1.SecretBackend_SECRET_BACKEND_VAULT))
		})

		It("Defaults unspecified backend to Vault", func() {
			created := createVaultSecret()
			Expect(created.GetSpec().GetBackend()).To(Equal(
				privatev1.SecretBackend_SECRET_BACKEND_VAULT))
		})

		It("Creates a Hub secret with coordinates", func() {
			created := createHubSecret()

			Expect(created.GetId()).ToNot(BeEmpty())
			Expect(created.GetSpec().GetBackend()).To(Equal(
				privatev1.SecretBackend_SECRET_BACKEND_HUB))
			Expect(created.GetSpec().GetCoordinates()).To(HaveKeyWithValue("cluster", "hub-1"))
			Expect(created.GetSpec().GetData()).To(BeEmpty())
		})

		It("List secrets", func() {
			const count = 5
			for i := range count {
				createVaultSecretWithName(fmt.Sprintf("secret-%d", i))
			}

			response, err := server.List(ctx, privatev1.SecretsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(count))
		})

		It("List secrets with limit", func() {
			const count = 5
			for i := range count {
				createVaultSecretWithName(fmt.Sprintf("secret-%d", i))
			}

			response, err := server.List(ctx, privatev1.SecretsListRequest_builder{
				Limit: new(int32(2)),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 2))
		})

		It("List secrets with offset", func() {
			const count = 5
			for i := range count {
				createVaultSecretWithName(fmt.Sprintf("secret-%d", i))
			}

			response, err := server.List(ctx, privatev1.SecretsListRequest_builder{
				Offset: new(int32(2)),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-2))
		})

		It("List secrets with filter", func() {
			const count = 3
			var ids []string
			for i := range count {
				obj := createVaultSecretWithName(fmt.Sprintf("secret-%d", i))
				ids = append(ids, obj.GetId())
			}

			for _, id := range ids {
				response, err := server.List(ctx, privatev1.SecretsListRequest_builder{
					Filter: new(fmt.Sprintf("this.id == '%s'", id)),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 1))
				Expect(response.GetItems()[0].GetId()).To(Equal(id))
			}
		})

		It("List secrets with order", func() {
			createVaultSecretWithName("aaa-secret")
			createVaultSecretWithName("zzz-secret")

			response, err := server.List(ctx, privatev1.SecretsListRequest_builder{
				Order: new("metadata.name asc"),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 2))
			Expect(response.GetItems()[0].GetMetadata().GetName()).To(Equal("aaa-secret"))
			Expect(response.GetItems()[1].GetMetadata().GetName()).To(Equal("zzz-secret"))
		})

		It("Update applies partial changes via field mask", func() {
			created := createVaultSecret()

			updateResponse, err := server.Update(ctx, privatev1.SecretsUpdateRequest_builder{
				Object: privatev1.Secret_builder{
					Id: created.GetId(),
					Spec: privatev1.SecretSpec_builder{
						Data: map[string][]byte{
							"new-key": []byte("new-value"),
						},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.data"}},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetSpec().GetData()).To(
				HaveKeyWithValue("new-key", []byte("new-value")))
		})

		It("Delete removes the object", func() {
			created := createVaultSecret()

			_, err := server.Delete(ctx, privatev1.SecretsDeleteRequest_builder{
				Id: created.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = server.Get(ctx, privatev1.SecretsGetRequest_builder{
				Id: created.GetId(),
			}.Build())
			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.NotFound))
		})

		It("Generates UUID for id ignoring caller-provided value", func() {
			callerProvidedId := "my-custom-id"
			response, err := server.Create(ctx, privatev1.SecretsCreateRequest_builder{
				Object: privatev1.Secret_builder{
					Id: callerProvidedId,
					Metadata: privatev1.Metadata_builder{
						Name: "my-secret",
					}.Build(),
					Spec: privatev1.SecretSpec_builder{
						Data: map[string][]byte{
							"key": []byte("value"),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetObject().GetId()).ToNot(Equal(callerProvidedId))
			_, err = uuid.Parse(response.GetObject().GetId())
			Expect(err).ToNot(HaveOccurred())
		})

		Describe("Validation", func() {
			It("Create without name fails", func() {
				_, err := server.Create(ctx, privatev1.SecretsCreateRequest_builder{
					Object: privatev1.Secret_builder{
						Spec: privatev1.SecretSpec_builder{
							Data: map[string][]byte{
								"key": []byte("value"),
							},
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				st, ok := status.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(st.Code()).To(Equal(codes.InvalidArgument))
				Expect(st.Message()).To(ContainSubstring("metadata.name"))
			})

			It("Create without spec fails", func() {
				_, err := server.Create(ctx, privatev1.SecretsCreateRequest_builder{
					Object: privatev1.Secret_builder{
						Metadata: privatev1.Metadata_builder{
							Name: "my-secret",
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				st, ok := status.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(st.Code()).To(Equal(codes.InvalidArgument))
				Expect(st.Message()).To(ContainSubstring("spec"))
			})

			It("Create Vault secret without data fails", func() {
				_, err := server.Create(ctx, privatev1.SecretsCreateRequest_builder{
					Object: privatev1.Secret_builder{
						Metadata: privatev1.Metadata_builder{
							Name: "my-secret",
						}.Build(),
						Spec: privatev1.SecretSpec_builder{
							Backend: privatev1.SecretBackend_SECRET_BACKEND_VAULT,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				st, ok := status.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(st.Code()).To(Equal(codes.InvalidArgument))
				Expect(st.Message()).To(ContainSubstring("spec.data"))
			})

			It("Create unspecified backend without data fails", func() {
				_, err := server.Create(ctx, privatev1.SecretsCreateRequest_builder{
					Object: privatev1.Secret_builder{
						Metadata: privatev1.Metadata_builder{
							Name: "my-secret",
						}.Build(),
						Spec: privatev1.SecretSpec_builder{}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				st, ok := status.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(st.Code()).To(Equal(codes.InvalidArgument))
				Expect(st.Message()).To(ContainSubstring("spec.data"))
			})

			It("Create Hub secret without coordinates fails", func() {
				_, err := server.Create(ctx, privatev1.SecretsCreateRequest_builder{
					Object: privatev1.Secret_builder{
						Metadata: privatev1.Metadata_builder{
							Name: "my-secret",
						}.Build(),
						Spec: privatev1.SecretSpec_builder{
							Backend: privatev1.SecretBackend_SECRET_BACKEND_HUB,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				st, ok := status.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(st.Code()).To(Equal(codes.InvalidArgument))
				Expect(st.Message()).To(ContainSubstring("spec.coordinates"))
			})

			It("Create Hub secret with data fails", func() {
				_, err := server.Create(ctx, privatev1.SecretsCreateRequest_builder{
					Object: privatev1.Secret_builder{
						Metadata: privatev1.Metadata_builder{
							Name: "my-secret",
						}.Build(),
						Spec: privatev1.SecretSpec_builder{
							Backend: privatev1.SecretBackend_SECRET_BACKEND_HUB,
							Coordinates: map[string]string{
								"cluster": "hub-1",
							},
							Data: map[string][]byte{
								"key": []byte("value"),
							},
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				st, ok := status.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(st.Code()).To(Equal(codes.InvalidArgument))
				Expect(st.Message()).To(ContainSubstring("spec.data"))
			})
		})

		Describe("Immutability", func() {
			It("Update changing backend fails", func() {
				created := createVaultSecret()

				_, err := server.Update(ctx, privatev1.SecretsUpdateRequest_builder{
					Object: privatev1.Secret_builder{
						Id: created.GetId(),
						Spec: privatev1.SecretSpec_builder{
							Backend: privatev1.SecretBackend_SECRET_BACKEND_HUB,
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.backend"}},
				}.Build())
				Expect(err).To(HaveOccurred())
				st, ok := status.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(st.Code()).To(Equal(codes.InvalidArgument))
				Expect(st.Message()).To(ContainSubstring("backend"))
				Expect(st.Message()).To(ContainSubstring("immutable"))
			})

			It("Update Hub secret with data fails", func() {
				created := createHubSecret()

				_, err := server.Update(ctx, privatev1.SecretsUpdateRequest_builder{
					Object: privatev1.Secret_builder{
						Id: created.GetId(),
						Spec: privatev1.SecretSpec_builder{
							Data: map[string][]byte{
								"key": []byte("should-not-be-allowed"),
							},
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.data"}},
				}.Build())
				Expect(err).To(HaveOccurred())
				st, ok := status.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(st.Code()).To(Equal(codes.InvalidArgument))
				Expect(st.Message()).To(ContainSubstring("spec.data"))
			})

			It("Update with metadata.tenant in update_mask fails", func() {
				created := createVaultSecret()

				_, err := server.Update(ctx, privatev1.SecretsUpdateRequest_builder{
					Object: privatev1.Secret_builder{
						Id: created.GetId(),
						Metadata: privatev1.Metadata_builder{
							Tenant: "other-tenant",
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"metadata.tenant"}},
				}.Build())
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("Name scoping", func() {
			It("Allows multiple secrets with same name in same tenant", func() {
				createVaultSecretWithName("shared-name")
				second := createVaultSecretWithName("shared-name")
				Expect(second.GetId()).ToNot(BeEmpty())
			})
		})

		Describe("Optimistic locking", func() {
			It("Update with stale version and lock=true fails", func() {
				created := createVaultSecret()

				_, err := server.Update(ctx, privatev1.SecretsUpdateRequest_builder{
					Object: privatev1.Secret_builder{
						Id: created.GetId(),
						Spec: privatev1.SecretSpec_builder{
							Data: map[string][]byte{
								"key": []byte("first-update"),
							},
						}.Build(),
					}.Build(),
					UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.data"}},
					Lock:       true,
				}.Build())
				Expect(err).ToNot(HaveOccurred())

				_, err = server.Update(ctx, privatev1.SecretsUpdateRequest_builder{
					Object: privatev1.Secret_builder{
						Id: created.GetId(),
						Spec: privatev1.SecretSpec_builder{
							Data: map[string][]byte{
								"key": []byte("second-update"),
							},
						}.Build(),
						Metadata: privatev1.Metadata_builder{
							Version: created.GetMetadata().GetVersion(),
						}.Build(),
					}.Build(),
					Lock: true,
				}.Build())
				Expect(err).To(HaveOccurred())
			})
		})

		It("Update without id fails", func() {
			_, err := server.Update(ctx, privatev1.SecretsUpdateRequest_builder{
				Object: privatev1.Secret_builder{
					Spec: privatev1.SecretSpec_builder{
						Data: map[string][]byte{
							"key": []byte("updated"),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.InvalidArgument))
			Expect(st.Message()).To(ContainSubstring("identifier"))
		})
	})

	It("Redacts event payload", func() {
		var event *privatev1.Event
		notifier := events.NewMockNotifier(ctrl)
		notifier.EXPECT().
			Notify(gomock.Any(), gomock.Any()).
			DoAndReturn(
				func(ctx context.Context, payload proto.Message) error {
					event = payload.(*privatev1.Event)
					return nil
				},
			)

		server, err := NewPrivateSecretsServer().
			SetLogger(logger).
			SetAttributionLogic(attribution).
			SetTenancyLogic(tenancy).
			SetNotifier(notifier).
			Build()
		Expect(err).ToNot(HaveOccurred())

		_, err = server.Create(
			ctx,
			privatev1.SecretsCreateRequest_builder{
				Object: privatev1.Secret_builder{
					Metadata: privatev1.Metadata_builder{
						Name: "redact-test",
					}.Build(),
					Spec: privatev1.SecretSpec_builder{
						Data: map[string][]byte{
							"password": []byte("super-secret"),
						},
					}.Build(),
				}.Build(),
			}.Build(),
		)
		Expect(err).ToNot(HaveOccurred())

		Expect(event).ToNot(BeNil())
		Expect(event.GetType()).To(Equal(privatev1.EventType_EVENT_TYPE_OBJECT_CREATED))
		object := event.GetSecret()
		Expect(object).ToNot(BeNil())
		Expect(object.GetSpec().GetData()).To(BeEmpty())
	})
})
