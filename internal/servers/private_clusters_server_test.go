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
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("Private clusters server", func() {
	var (
		ctx context.Context
		tx  database.Tx
	)

	BeforeEach(func() {
		var err error

		// Create a context:
		ctx = context.Background()

		// Prepare the database pool:
		db := server.MakeDatabase()
		DeferCleanup(db.Close)
		pool, err := pgxpool.New(ctx, db.MakeURL())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)

		// Create the transaction manager:
		tm, err := database.NewTxManager().
			SetLogger(logger).
			SetPool(pool).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Start a transaction and add it to the context:
		tx, err = tm.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := tm.End(ctx, tx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)

		// Create the tables:
		err = dao.CreateTables(
			ctx,
			"cluster_templates",
			"clusters",
			"host_classes",
		)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivateClustersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivateClustersServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPrivateClustersServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivateClustersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var server *PrivateClustersServer

		BeforeEach(func() {
			var err error

			// Create the server:
			server, err = NewPrivateClustersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Ceate the host classes DAO:
			hoastClassesDao, err := dao.NewGenericDAO[*privatev1.HostClass]().
				SetLogger(logger).
				SetTable("host_classes").
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create the templates DAO:
			templatesDao, err := dao.NewGenericDAO[*privatev1.ClusterTemplate]().
				SetLogger(logger).
				SetTable("cluster_templates").
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create the host classes:
			Expect(err).ToNot(HaveOccurred())
			_, err = hoastClassesDao.Create().
				SetObject(
					privatev1.HostClass_builder{
						Id: "acme-1ti-id",
						Metadata: privatev1.Metadata_builder{
							Name: "acme-1ti-name",
						}.Build(),
						Title:       "ACME 1TiB",
						Description: "ACME 1TiB.",
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			_, err = hoastClassesDao.Create().
				SetObject(
					privatev1.HostClass_builder{
						Id: "acme-gpu-id",
						Metadata: privatev1.Metadata_builder{
							Name: "acme-gpu-name",
						}.Build(),
						Title:       "ACME GPU",
						Description: "ACME GPU.",
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create a usable template:
			_, err = templatesDao.Create().
				SetObject(
					privatev1.ClusterTemplate_builder{
						Id: "my-template-id",
						Metadata: privatev1.Metadata_builder{
							Name: "my-template-name",
						}.Build(),
						Title:       "My template",
						Description: "My template",
						NodeSets: map[string]*privatev1.ClusterTemplateNodeSet{
							"compute": privatev1.ClusterTemplateNodeSet_builder{
								HostClass: "acme-1ti-id",
								Size:      3,
							}.Build(),
							"gpu": privatev1.ClusterTemplateNodeSet_builder{
								HostClass: "acme-gpu-id",
								Size:      1,
							}.Build(),
						},
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create numbered templates for list tests:
			for i := range 10 {
				_, err = templatesDao.Create().
					SetObject(
						privatev1.ClusterTemplate_builder{
							Id:          fmt.Sprintf("my-template-id-%d", i),
							Title:       fmt.Sprintf("My template %d", i),
							Description: fmt.Sprintf("My template %d", i),
							NodeSets: map[string]*privatev1.ClusterTemplateNodeSet{
								"compute": privatev1.ClusterTemplateNodeSet_builder{
									HostClass: "acme-1ti-id",
									Size:      3,
								}.Build(),
							},
						}.Build(),
					).
					Do(ctx)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		It("Creates object", func() {
			response, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())
		})

		It("Creates object with template specified by name", func() {
			// Create the object:
			response, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-name",
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())

			// Verify that the template name was replaced by the identifier:
			Expect(object.GetSpec().GetTemplate()).To(Equal("my-template-id"))
		})

		It("Fails when creating object with non-existent template name", func() {
			_, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "does-not-exist",
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.NotFound))
			Expect(status.Message()).To(Equal(
				"there is no template with identifier or name 'does-not-exist'",
			))
		})

		It("Creates object with host class specified by name in node set", func() {
			// Create a cluster specifying the host class by name:
			response, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
						NodeSets: map[string]*privatev1.ClusterNodeSet{
							"compute": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-1ti-name",
								Size:      5,
							}.Build(),
						},
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())

			// Verify that the host class name was replaced by the identifier:
			nodeSets := object.GetSpec().GetNodeSets()
			Expect(nodeSets).To(HaveKey("compute"))
			nodeSet := nodeSets["compute"]
			Expect(nodeSet.GetHostClass()).To(Equal("acme-1ti-id"))
		})

		It("Creates object with host class specified by identifier in node set", func() {
			// Create a cluster specifying the host class by identifier:
			response, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
						NodeSets: map[string]*privatev1.ClusterNodeSet{
							"compute": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-1ti-id",
								Size:      7,
							}.Build(),
						},
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())

			// Verify that the host class identifier is preserved:
			nodeSets := object.GetSpec().GetNodeSets()
			Expect(nodeSets).To(HaveKey("compute"))
			nodeSet := nodeSets["compute"]
			Expect(nodeSet.GetHostClass()).To(Equal("acme-1ti-id"))
		})

		It("Creates object with template and host class specified by name", func() {
			// Create a cluster specifying the template and the host class by name:
			response, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-name",
						NodeSets: map[string]*privatev1.ClusterNodeSet{
							"compute": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-1ti-name",
								Size:      7,
							}.Build(),
						},
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())

			// Verify that the the template and host class names were replaced by the identifiers:
			Expect(object.GetSpec().GetTemplate()).To(Equal("my-template-id"))
			nodeSets := object.GetSpec().GetNodeSets()
			Expect(nodeSets).To(HaveKey("compute"))
			nodeSet := nodeSets["compute"]
			Expect(nodeSet.GetHostClass()).To(Equal("acme-1ti-id"))
		})

		It("Fails when creating object with non-existent host class name", func() {
			_, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
						NodeSets: map[string]*privatev1.ClusterNodeSet{
							"compute": privatev1.ClusterNodeSet_builder{
								HostClass: `does-not-exist`,
								Size:      5,
							}.Build(),
						},
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.NotFound))
			Expect(status.Message()).To(Equal(
				"there is no host class with identifier or name 'does-not-exist'",
			))
		})

		It("Fails when creating object with non-existent node set", func() {
			_, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
						NodeSets: map[string]*privatev1.ClusterNodeSet{
							"does-not-exist": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-1ti-id",
								Size:      5,
							}.Build(),
						},
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(
				"node set 'does-not-exist' doesn't exist, valid values for template 'my-template-id' " +
					"are 'compute' and 'gpu'",
			))
		})

		It("Fails when creating object with host class that doesn't match template", func() {
			_, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
						NodeSets: map[string]*privatev1.ClusterNodeSet{
							"compute": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-gpu-id",
								Size:      5,
							}.Build(),
						},
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(
				"host class for node set 'compute' should be empty, 'acme-1ti-name' or 'acme-1ti-id', " +
					"like in template 'my-template-id', but it is 'acme-gpu-id'",
			))
		})

		It("Returns 'already exists' when creating object with existing identifier", func() {
			// Create an object with a specific identifier:
			id := uuid.New()
			_, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Id: id,
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Try to create another object with the same identifier:
			_, err = server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Id: id,
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.AlreadyExists))
			Expect(status.Message()).To(Equal(fmt.Sprintf("object with identifier '%s' already exists", id)))
		})

		It("List objects", func() {
			// Create a few objects:
			const count = 10
			for i := range count {
				_, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
					Object: privatev1.Cluster_builder{
						Spec: privatev1.ClusterSpec_builder{
							Template: fmt.Sprintf("my-template-id-%d", i),
						}.Build(),
						Status: privatev1.ClusterStatus_builder{
							Hub: fmt.Sprintf("my-hub-id-%d", i),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, privatev1.ClustersListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			// Create a few objects:
			const count = 10
			for i := range count {
				_, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
					Object: privatev1.Cluster_builder{
						Spec: privatev1.ClusterSpec_builder{
							Template: fmt.Sprintf("my-template-id-%d", i),
						}.Build(),
						Status: privatev1.ClusterStatus_builder{
							Hub: fmt.Sprintf("my-hub-id-%d", i),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, privatev1.ClustersListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			// Create a few objects:
			const count = 10
			for i := range count {
				_, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
					Object: privatev1.Cluster_builder{
						Spec: privatev1.ClusterSpec_builder{
							Template: fmt.Sprintf("my-template-id-%d", i),
						}.Build(),
						Status: privatev1.ClusterStatus_builder{
							Hub: fmt.Sprintf("my-hub-id-%d", i),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, privatev1.ClustersListRequest_builder{
				Offset: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-1))
		})

		It("List objects with filter", func() {
			// Create a few objects:
			const count = 10
			var objects []*privatev1.Cluster
			for i := range count {
				response, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
					Object: privatev1.Cluster_builder{
						Spec: privatev1.ClusterSpec_builder{
							Template: fmt.Sprintf("my-template-id-%d", i),
						}.Build(),
						Status: privatev1.ClusterStatus_builder{
							Hub: fmt.Sprintf("my-hub-%d", i),
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				objects = append(objects, response.GetObject())
			}

			// List the objects:
			for _, object := range objects {
				response, err := server.List(ctx, privatev1.ClustersListRequest_builder{
					Filter: proto.String(fmt.Sprintf("this.id == '%s'", object.GetId())),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 1))
				Expect(response.GetItems()[0].GetId()).To(Equal(object.GetId()))
			}
		})

		It("Get object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get it:
			getResponse, err := server.Get(ctx, privatev1.ClustersGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(createResponse.GetObject(), getResponse.GetObject())).To(BeTrue())
		})

		It("Update object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Update the object (keeping template unchanged):
			updateResponse, err := server.Update(ctx, privatev1.ClustersUpdateRequest_builder{
				Object: privatev1.Cluster_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterSpec_builder{
						Template: object.GetSpec().GetTemplate(),
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "your_hub",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetSpec().GetTemplate()).To(Equal("my-template-id"))
			Expect(updateResponse.GetObject().GetStatus().GetHub()).To(Equal("your_hub"))

			// Get and verify:
			getResponse, err := server.Get(ctx, privatev1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetSpec().GetTemplate()).To(Equal("my-template-id"))
			Expect(getResponse.GetObject().GetStatus().GetHub()).To(Equal("your_hub"))
		})

		It("Delete object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Metadata: privatev1.Metadata_builder{
						Finalizers: []string{"a"},
					}.Build(),
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Hub: "my-hub-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Delete the object:
			_, err = server.Delete(ctx, privatev1.ClustersDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get and verify:
			getResponse, err := server.Get(ctx, privatev1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})

		It("Rejects creation with duplicate condition", func() {
			_, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Conditions: []*privatev1.ClusterCondition{
							privatev1.ClusterCondition_builder{
								Type: privatev1.ClusterConditionType_CLUSTER_CONDITION_TYPE_READY,
							}.Build(),
							privatev1.ClusterCondition_builder{
								Type: privatev1.ClusterConditionType_CLUSTER_CONDITION_TYPE_READY,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal("condition 'CLUSTER_CONDITION_TYPE_READY' is duplicated"))
		})

		It("Rejects update with duplicate condition", func() {
			_, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
					Status: privatev1.ClusterStatus_builder{
						Conditions: []*privatev1.ClusterCondition{
							privatev1.ClusterCondition_builder{
								Type: privatev1.ClusterConditionType_CLUSTER_CONDITION_TYPE_READY,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			_, err = server.Update(ctx, privatev1.ClustersUpdateRequest_builder{
				Object: privatev1.Cluster_builder{
					Status: privatev1.ClusterStatus_builder{
						Conditions: []*privatev1.ClusterCondition{
							privatev1.ClusterCondition_builder{
								Type: privatev1.ClusterConditionType_CLUSTER_CONDITION_TYPE_READY,
							}.Build(),
							privatev1.ClusterCondition_builder{
								Type: privatev1.ClusterConditionType_CLUSTER_CONDITION_TYPE_READY,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal("condition 'CLUSTER_CONDITION_TYPE_READY' is duplicated"))
		})

		It("Allows adding a new node set", func() {
			// Create a cluster with the default node sets from the template
			createResponse, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			Expect(object.GetSpec().GetNodeSets()).To(HaveLen(2)) // compute and gpu

			// Add a new node set
			_, err = server.Update(ctx, privatev1.ClustersUpdateRequest_builder{
				Object: privatev1.Cluster_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterSpec_builder{
						NodeSets: map[string]*privatev1.ClusterNodeSet{
							"compute": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-1ti-id",
								Size:      3,
							}.Build(),
							"gpu": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-gpu-id",
								Size:      1,
							}.Build(),
							"storage": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-1ti-id",
								Size:      2,
							}.Build(),
						},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.node_sets"},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows removing a node set when multiple exist", func() {
			// Create a cluster with the default node sets from the template
			createResponse, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			Expect(object.GetSpec().GetNodeSets()).To(HaveLen(2)) // compute and gpu

			// Remove the gpu node set
			_, err = server.Update(ctx, privatev1.ClustersUpdateRequest_builder{
				Object: privatev1.Cluster_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterSpec_builder{
						NodeSets: map[string]*privatev1.ClusterNodeSet{
							"compute": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-1ti-id",
								Size:      3,
							}.Build(),
						},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.node_sets"},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Rejects removing the last node set", func() {
			// Create a cluster with a template that has only one node set
			createResponse, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id-0",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			Expect(object.GetSpec().GetNodeSets()).To(HaveLen(1)) // only compute

			// Try to remove the last node set
			_, err = server.Update(ctx, privatev1.ClustersUpdateRequest_builder{
				Object: privatev1.Cluster_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterSpec_builder{
						NodeSets: map[string]*privatev1.ClusterNodeSet{},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.node_sets"},
				},
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal("cannot remove the last node set: clusters must have at least one node set"))
		})

		It("Rejects changing host_class of an existing node set", func() {
			// Create a cluster with the default node sets from the template
			createResponse, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Try to change the host_class of the compute node set
			_, err = server.Update(ctx, privatev1.ClustersUpdateRequest_builder{
				Object: privatev1.Cluster_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterSpec_builder{
						NodeSets: map[string]*privatev1.ClusterNodeSet{
							"compute": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-gpu-id", // Changed from acme-1ti-id
								Size:      3,
							}.Build(),
							"gpu": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-gpu-id",
								Size:      1,
							}.Build(),
						},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.node_sets"},
				},
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal("cannot change host_class for node set 'compute' from 'acme-1ti-id' to 'acme-gpu-id': host_class is immutable"))
		})

		It("Allows changing size of an existing node set", func() {
			// Create a cluster with the default node sets from the template
			createResponse, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Change the size of the compute node set
			updateResponse, err := server.Update(ctx, privatev1.ClustersUpdateRequest_builder{
				Object: privatev1.Cluster_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterSpec_builder{
						NodeSets: map[string]*privatev1.ClusterNodeSet{
							"compute": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-1ti-id",
								Size:      5, // Changed from 3
							}.Build(),
							"gpu": privatev1.ClusterNodeSet_builder{
								HostClass: "acme-gpu-id",
								Size:      1,
							}.Build(),
						},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.node_sets"},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			updatedObject := updateResponse.GetObject()
			Expect(updatedObject.GetSpec().GetNodeSets()["compute"].GetSize()).To(Equal(int32(5)))
		})

		It("Allows changing size with granular field mask", func() {
			// Create a cluster with the default node sets from the template
			createResponse, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Get the initial size
			initialSize := object.GetSpec().GetNodeSets()["compute"].GetSize()
			newSize := initialSize + 2

			// Change only the size using a granular field mask
			updateResponse, err := server.Update(ctx, privatev1.ClustersUpdateRequest_builder{
				Object: privatev1.Cluster_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterSpec_builder{
						NodeSets: map[string]*privatev1.ClusterNodeSet{
							"compute": privatev1.ClusterNodeSet_builder{
								Size: newSize,
							}.Build(),
						},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.node_sets.compute.size"},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			updatedObject := updateResponse.GetObject()
			Expect(updatedObject.GetSpec().GetNodeSets()["compute"].GetSize()).To(Equal(newSize))
		})

		It("Rejects changing template field", func() {
			oldTemplate := "my-template-id"
			newTemplate := "my-template-id-0"

			// Create a cluster
			createResponse, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: oldTemplate,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Try to change the template
			_, err = server.Update(ctx, privatev1.ClustersUpdateRequest_builder{
				Object: privatev1.Cluster_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterSpec_builder{
						Template: newTemplate,
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.template"},
				},
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(fmt.Sprintf(
				"cannot change spec.template from '%s' to '%s': template is immutable",
				oldTemplate, newTemplate,
			)))
		})

		It("Rejects changing template_parameters field", func() {
			// Create a cluster with template parameters
			createResponse, err := server.Create(ctx, privatev1.ClustersCreateRequest_builder{
				Object: privatev1.Cluster_builder{
					Spec: privatev1.ClusterSpec_builder{
						Template: "my-template-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Try to change the template parameters
			_, err = server.Update(ctx, privatev1.ClustersUpdateRequest_builder{
				Object: privatev1.Cluster_builder{
					Id: object.GetId(),
					Spec: privatev1.ClusterSpec_builder{
						Template:           object.GetSpec().GetTemplate(),
						TemplateParameters: map[string]*anypb.Any{"key": nil},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.template_parameters"},
				},
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal("cannot change spec.template_parameters: template parameters are immutable"))
		})
	})
})
