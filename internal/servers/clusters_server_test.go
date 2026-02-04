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
	"google.golang.org/protobuf/types/known/wrapperspb"

	ffv1 "github.com/innabox/fulfillment-service/internal/api/fulfillment/v1"
	privatev1 "github.com/innabox/fulfillment-service/internal/api/private/v1"
	sharedv1 "github.com/innabox/fulfillment-service/internal/api/shared/v1"
	"github.com/innabox/fulfillment-service/internal/database"
	"github.com/innabox/fulfillment-service/internal/database/dao"
)

var _ = Describe("Clusters server", func() {
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
			server, err := NewClustersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewClustersServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewClustersServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewClustersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var server *ClustersServer

		makeAny := func(m proto.Message) *anypb.Any {
			a, err := anypb.New(m)
			Expect(err).ToNot(HaveOccurred())
			return a
		}

		BeforeEach(func() {
			var err error

			// Create the server:
			server, err = NewClustersServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create the host classes DAO:
			hostClassesDao, err := dao.NewGenericDAO[*privatev1.HostClass]().
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

			// Create the the host classes:
			_, err = hostClassesDao.Create().SetObject(privatev1.HostClass_builder{
				Id:          "acme_1tib",
				Title:       "ACME 1TiB",
				Description: "ACME 1TiB.",
			}.Build()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			_, err = hostClassesDao.Create().SetObject(privatev1.HostClass_builder{
				Id:          "acme_gpu",
				Title:       "ACME GPU",
				Description: "ACME GPU.",
			}.Build()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			_, err = hostClassesDao.Create().SetObject(privatev1.HostClass_builder{
				Id:          "hal_9000",
				Title:       "HAL 9000",
				Description: "Heuristically programmed ALgorithmic computer.",
			}.Build()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create a usable template:
			_, err = templatesDao.Create().SetObject(privatev1.ClusterTemplate_builder{
				Id:          "my_template",
				Title:       "My template",
				Description: "My template",
				NodeSets: map[string]*privatev1.ClusterTemplateNodeSet{
					"compute": privatev1.ClusterTemplateNodeSet_builder{
						HostClass: "acme_1tib",
						Size:      3,
					}.Build(),
					"gpu": privatev1.ClusterTemplateNodeSet_builder{
						HostClass: "acme_gpu",
						Size:      1,
					}.Build(),
				},
			}.Build()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create a template that has been deleted. Note that we add a finalizer to ensure that it will
			// not be completely deleted and archived, as we will use it to verify that clusters can't be
			// created using deleted templates.
			_, err = templatesDao.Create().SetObject(privatev1.ClusterTemplate_builder{
				Id:          "my_deleted_template",
				Title:       "My deleted template",
				Description: "My deleted template",
				Metadata: privatev1.Metadata_builder{
					Finalizers: []string{"a"},
				}.Build(),
			}.Build()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			_, err = templatesDao.Delete().SetId("my_deleted_template").Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create a template with parameters:
			_, err = templatesDao.Create().SetObject(privatev1.ClusterTemplate_builder{
				Id:          "my_with_parameters",
				Title:       "My with parameters",
				Description: "My with parameters.",
				Parameters: []*privatev1.ClusterTemplateParameterDefinition{
					privatev1.ClusterTemplateParameterDefinition_builder{
						Name:        "my_required_bool",
						Title:       "My required bool",
						Description: "My required bool.",
						Required:    true,
						Type:        "type.googleapis.com/google.protobuf.BoolValue",
					}.Build(),
					privatev1.ClusterTemplateParameterDefinition_builder{
						Name:        "my_optional_string",
						Title:       "My optional string",
						Description: "My optional string.",
						Required:    false,
						Type:        "type.googleapis.com/google.protobuf.StringValue",
						Default:     makeAny(wrapperspb.String("my value")),
					}.Build(),
				},
			}.Build()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Creates object", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())
		})

		It("Doesn't create object without template", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal("template is mandatory"))
		})

		It("Takes default node sets from template", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()
			nodeSets := object.GetSpec().GetNodeSets()
			Expect(nodeSets).To(HaveKey("compute"))
			computeNodeSet := nodeSets["compute"]
			Expect(computeNodeSet.GetHostClass()).To(Equal("acme_1tib"))
			Expect(computeNodeSet.GetSize()).To(BeNumerically("==", 3))
			Expect(nodeSets).To(HaveKey("gpu"))
			gpuNodeSet := nodeSets["gpu"]
			Expect(gpuNodeSet.GetHostClass()).To(Equal("acme_gpu"))
			Expect(gpuNodeSet.GetSize()).To(BeNumerically("==", 1))
		})

		It("Rejects node set that isn't in the template", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
						NodeSets: map[string]*ffv1.ClusterNodeSet{
							"junk": ffv1.ClusterNodeSet_builder{
								HostClass: "acme_1tib",
								Size:      1000,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(
				"node set 'junk' doesn't exist, valid values for template 'my_template' are " +
					"'compute' and 'gpu'",
			))
		})

		It("Rejects node set with host class that isn't in the template", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
						NodeSets: map[string]*ffv1.ClusterNodeSet{
							"compute": ffv1.ClusterNodeSet_builder{
								HostClass: "hal_9000",
								Size:      1000,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(
				"host class for node set 'compute' should be empty or 'acme_1tib', like in " +
					"template 'my_template', but it is 'hal_9000'",
			))
		})

		It("Rejects node set with zero size", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
						NodeSets: map[string]*ffv1.ClusterNodeSet{
							"compute": ffv1.ClusterNodeSet_builder{
								Size: 0,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(
				"size for node set 'compute' should be greater than zero, but it is 0",
			))
		})

		It("Rejects node set with negative size", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
						NodeSets: map[string]*ffv1.ClusterNodeSet{
							"compute": ffv1.ClusterNodeSet_builder{
								Size: -1,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(
				"size for node set 'compute' should be greater than zero, but it is -1",
			))
		})

		It("Accepts node set with explicit size", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
						NodeSets: map[string]*ffv1.ClusterNodeSet{
							"compute": ffv1.ClusterNodeSet_builder{
								Size: 1000,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()
			nodeSets := object.GetSpec().GetNodeSets()
			Expect(nodeSets).To(HaveKey("compute"))
			nodeSet := nodeSets["compute"]
			Expect(nodeSet.GetSize()).To(BeNumerically("==", 1000))
		})

		It("Accepts multiple node sets with explicit size", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
						NodeSets: map[string]*ffv1.ClusterNodeSet{
							"compute": ffv1.ClusterNodeSet_builder{
								Size: 30,
							}.Build(),
							"gpu": ffv1.ClusterNodeSet_builder{
								Size: 10,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()
			nodeSets := object.GetSpec().GetNodeSets()
			Expect(nodeSets).To(HaveKey("compute"))
			computeNodeSet := nodeSets["compute"]
			Expect(computeNodeSet.GetSize()).To(BeNumerically("==", 30))
			Expect(nodeSets).To(HaveKey("gpu"))
			gpuNodeSet := nodeSets["gpu"]
			Expect(gpuNodeSet.GetSize()).To(BeNumerically("==", 10))
		})

		It("Merges explicit size for one node set with size for another node set from the template", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
						NodeSets: map[string]*ffv1.ClusterNodeSet{
							"compute": ffv1.ClusterNodeSet_builder{
								Size: 30,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()
			nodeSets := object.GetSpec().GetNodeSets()
			Expect(nodeSets).To(HaveKey("compute"))
			computeNodeSet := nodeSets["compute"]
			Expect(computeNodeSet.GetSize()).To(BeNumerically("==", 30))
			Expect(nodeSets).To(HaveKey("gpu"))
			gpuNodeSet := nodeSets["gpu"]
			Expect(gpuNodeSet.GetSize()).To(BeNumerically("==", 1))
		})

		It("Rejects template that has been deleted", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_deleted_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(
				"template 'my_deleted_template' has been deleted",
			))
		})

		It("Doesn't create object if there are missing required template parameters", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_with_parameters",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(
				"parameter 'my_required_bool' of template 'my_with_parameters' is mandatory",
			))
		})

		It("Doesn't create object if one parameter doesn't exist in the template", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_with_parameters",
						TemplateParameters: map[string]*anypb.Any{
							"junk": makeAny(wrapperspb.Int32(123)),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(
				"template parameter 'junk' doesn't exist, valid values for template " +
					"'my_with_parameters' are 'my_optional_string' and 'my_required_bool'",
			))
		})

		It("Doesn't create object if two parameters don't exist in the template", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_with_parameters",
						TemplateParameters: map[string]*anypb.Any{
							"junk1": makeAny(wrapperspb.Int32(123)),
							"junk2": makeAny(wrapperspb.Int32(123)),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(
				"template parameters 'junk1' and 'junk2' don't exist, valid values for template " +
					"'my_with_parameters' are 'my_optional_string' and 'my_required_bool'",
			))
		})

		It("Doesn't create object if parameter type doesn't match the template", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_with_parameters",
						TemplateParameters: map[string]*anypb.Any{
							"my_required_bool": makeAny(wrapperspb.Int32(123)),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(Equal(
				"type of parameter 'my_required_bool' of template 'my_with_parameters' should be " +
					"'type.googleapis.com/google.protobuf.BoolValue', but it is " +
					"'type.googleapis.com/google.protobuf.Int32Value'",
			))
		})

		It("Takes default values of parameters from the template", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_with_parameters",
						TemplateParameters: map[string]*anypb.Any{
							"my_required_bool": makeAny(wrapperspb.Bool(true)),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()
			templateParameters := object.GetSpec().GetTemplateParameters()

			parameterValue := templateParameters["my_required_bool"]
			Expect(parameterValue).ToNot(BeNil())
			boolValue := &wrapperspb.BoolValue{}
			err = anypb.UnmarshalTo(parameterValue, boolValue, proto.UnmarshalOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(boolValue.GetValue()).To(BeTrue())

			parameterValue = templateParameters["my_optional_string"]
			Expect(parameterValue).ToNot(BeNil())
			stringValue := &wrapperspb.StringValue{}
			err = anypb.UnmarshalTo(parameterValue, stringValue, proto.UnmarshalOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(stringValue.GetValue()).To(Equal("my value"))
		})

		It("Allows overriding of default values of template parameters", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_with_parameters",
						TemplateParameters: map[string]*anypb.Any{
							"my_required_bool":   makeAny(wrapperspb.Bool(false)),
							"my_optional_string": makeAny(wrapperspb.String("your value")),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := response.GetObject()
			templateParameters := object.GetSpec().GetTemplateParameters()
			parameterValue := templateParameters["my_optional_string"]
			Expect(parameterValue).ToNot(BeNil())
			stringValue := &wrapperspb.StringValue{}
			err = anypb.UnmarshalTo(parameterValue, stringValue, proto.UnmarshalOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(stringValue.GetValue()).To(Equal("your value"))
		})

		It("List objects", func() {
			// Create a few objects:
			const count = 10
			for range count {
				_, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
					Object: ffv1.Cluster_builder{
						Spec: ffv1.ClusterSpec_builder{
							Template: "my_template",
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, ffv1.ClustersListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			// Create a few objects:
			const count = 10
			for range count {
				_, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
					Object: ffv1.Cluster_builder{
						Spec: ffv1.ClusterSpec_builder{
							Template: "my_template",
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, ffv1.ClustersListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			// Create a few objects:
			const count = 10
			for range count {
				_, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
					Object: ffv1.Cluster_builder{
						Spec: ffv1.ClusterSpec_builder{
							Template: "my_template",
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, ffv1.ClustersListRequest_builder{
				Offset: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-1))
		})

		It("List objects with filter", func() {
			// Create a few objects:
			const count = 10
			var objects []*ffv1.Cluster
			for range count {
				response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
					Object: ffv1.Cluster_builder{
						Spec: ffv1.ClusterSpec_builder{
							Template: "my_template",
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				objects = append(objects, response.GetObject())
			}

			// List the objects:
			for _, object := range objects {
				response, err := server.List(ctx, ffv1.ClustersListRequest_builder{
					Filter: proto.String(fmt.Sprintf("this.id == '%s'", object.GetId())),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 1))
				Expect(response.GetItems()[0].GetId()).To(Equal(object.GetId()))
			}
		})

		It("Get object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get it:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(proto.Equal(createResponse.GetObject(), getResponse.GetObject())).To(BeTrue())
		})

		It("Returns not found error when getting object that doesn't exist", func() {
			response, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: "does-not-exist",
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.NotFound))
		})

		It("Update object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Update the object:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Spec: ffv1.ClusterSpec_builder{
						NodeSets: map[string]*ffv1.ClusterNodeSet{
							"compute": ffv1.ClusterNodeSet_builder{
								HostClass: "acme_1tib",
								Size:      4,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			nodeSet := object.GetSpec().GetNodeSets()["compute"]
			Expect(nodeSet.GetHostClass()).To(Equal("acme_1tib"))
			Expect(nodeSet.GetSize()).To(BeNumerically("==", 4))

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			nodeSet = object.GetSpec().GetNodeSets()["compute"]
			Expect(nodeSet.GetHostClass()).To(Equal("acme_1tib"))
			Expect(nodeSet.GetSize()).To(BeNumerically("==", 4))
		})

		It("Ignores changes to the status when an object is updated", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Try to update the status:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Status: ffv1.ClusterStatus_builder{
						ApiUrl:     "https://my.api.com",
						ConsoleUrl: "https://my.console.com",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			Expect(object.GetStatus().GetApiUrl()).To(BeEmpty())
			Expect(object.GetStatus().GetConsoleUrl()).To(BeEmpty())

			// Get the response and verify that the status hasn't been updated:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetStatus().GetApiUrl()).To(BeEmpty())
			Expect(object.GetStatus().GetConsoleUrl()).To(BeEmpty())
		})

		It("Delete object", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Add a finalizer, as otherwise the object will be immediatelly deleted and archived and it
			// won't be possible to verify the deletion timestamp. This can't be done using the server
			// because this is a public object, and public objects don't have the finalizers field.
			_, err = tx.Exec(
				ctx,
				`update clusters set finalizers = '{"a"}' where id = $1`,
				object.GetId(),
			)
			Expect(err).ToNot(HaveOccurred())

			// Delete the object:
			_, err = server.Delete(ctx, ffv1.ClustersDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})

		It("Returns not found error when deleting object that doesn't exist", func() {
			response, err := server.Delete(ctx, ffv1.ClustersDeleteRequest_builder{
				Id: "does_not_exist",
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.NotFound))
		})

		It("Preserves private data during update", func() {
			// Create the DAO:
			dao, err := dao.NewGenericDAO[*privatev1.Cluster]().
				SetLogger(logger).
				SetTable("clusters").
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Use the DAO directly to create an object with private data:
			createResponse, err := dao.Create().
				SetObject(
					privatev1.Cluster_builder{
						Spec: privatev1.ClusterSpec_builder{
							Template: "my_template",
							NodeSets: map[string]*privatev1.ClusterNodeSet{
								"compute": privatev1.ClusterNodeSet_builder{
									HostClass: "my_host_class",
									Size:      3,
								}.Build(),
							},
						}.Build(),
						Status: privatev1.ClusterStatus_builder{
							Hub: "123",
						}.
							Build(),
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Update the object using the public server:
			_, err = server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
						NodeSets: map[string]*ffv1.ClusterNodeSet{
							"compute": ffv1.ClusterNodeSet_builder{
								HostClass: "my_host_class",
								Size:      4,
							}.Build(),
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get the object again and verify that the private data hasn't changed:
			getResponse, err := dao.Get().
				SetId(object.GetId()).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(err).ToNot(HaveOccurred())
			Expect(object.GetStatus().GetHub()).To(Equal("123"))
		})

		It("Ignores status during creation", func() {
			// Try to create an object with status:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
					Status: ffv1.ClusterStatus_builder{
						ApiUrl: "https://your.api",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Get the object and verify that the status was ignored:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetStatus().GetApiUrl()).To(BeEmpty())
		})

		It("Ignores changes to status during update", func() {
			// Use the DAO directly to create an object with status:
			dao, err := dao.NewGenericDAO[*privatev1.Cluster]().
				SetLogger(logger).
				SetTable("clusters").
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			createResponse, err := dao.Create().
				SetObject(
					privatev1.Cluster_builder{
						Spec: privatev1.ClusterSpec_builder{
							Template: "my_template",
						}.Build(),
						Status: privatev1.ClusterStatus_builder{
							ApiUrl: "https://my.api",
						}.Build(),
					}.Build(),
				).
				Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Try to update the status:
			_, err = server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
					Status: ffv1.ClusterStatus_builder{
						ApiUrl: "https://your.api",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get the object again and verify that the status hasn't changed:
			getResponse, err := dao.Get().SetId(object.GetId()).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetStatus().GetApiUrl()).To(Equal("https://my.api"))
		})

		It("Update object with mask", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Update the object using the field mask:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Spec: ffv1.ClusterSpec_builder{
						Template: "your_template",
						NodeSets: map[string]*ffv1.ClusterNodeSet{
							"compute": ffv1.ClusterNodeSet_builder{
								Size: 4,
							}.Build(),
							"gpu": ffv1.ClusterNodeSet_builder{
								Size: 2,
							}.Build(),
						},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"spec.node_sets.compute.size",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			verify := func(object *ffv1.Cluster) {
				Expect(object.GetSpec().GetTemplate()).To(Equal("my_template"))
				computeNodeSet := object.GetSpec().GetNodeSets()["compute"]
				Expect(computeNodeSet.GetSize()).To(BeNumerically("==", 4))
				gpuNodeSet := object.GetSpec().GetNodeSets()["gpu"]
				Expect(gpuNodeSet.GetSize()).To(BeNumerically("==", 1))
			}
			verify(object)

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			verify(object)
		})

		It("Allows removing a node set when multiple exist", func() {
			// Create a cluster with the default node sets from the template (compute and gpu):
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			Expect(object.GetSpec().GetNodeSets()).To(HaveLen(2))
			Expect(object.GetSpec().GetNodeSets()).To(HaveKey("compute"))
			Expect(object.GetSpec().GetNodeSets()).To(HaveKey("gpu"))

			// Remove the gpu node set by updating with only the compute node set:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Spec: ffv1.ClusterSpec_builder{
						NodeSets: map[string]*ffv1.ClusterNodeSet{
							"compute": ffv1.ClusterNodeSet_builder{
								HostClass: "acme_1tib",
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
			object = updateResponse.GetObject()
			Expect(object.GetSpec().GetNodeSets()).To(HaveLen(1))
			Expect(object.GetSpec().GetNodeSets()).To(HaveKey("compute"))
			Expect(object.GetSpec().GetNodeSets()).ToNot(HaveKey("gpu"))

			// Get and verify the node set was removed:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			Expect(object.GetSpec().GetNodeSets()).To(HaveLen(1))
			Expect(object.GetSpec().GetNodeSets()).To(HaveKey("compute"))
			Expect(object.GetSpec().GetNodeSets()).ToNot(HaveKey("gpu"))
		})

		It("Sets name when creating", func() {
			response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Metadata: sharedv1.Metadata_builder{
						Name: "my-cluster",
					}.Build(),
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetObject().GetMetadata().GetName()).To(Equal("my-cluster"))

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: response.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetName()).To(Equal("my-cluster"))
		})

		It("Updates name", func() {
			// Create the object with an initial name:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Metadata: sharedv1.Metadata_builder{
						Name: "my-name",
					}.Build(),
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(createResponse.GetObject().GetMetadata().GetName()).To(Equal("my-name"))

			// Update the name:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: createResponse.GetObject().GetId(),
					Metadata: sharedv1.Metadata_builder{
						Name: "your-name",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetMetadata().GetName()).To(Equal("your-name"))

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetName()).To(Equal("your-name"))
		})

		It("Returns not found error when updating object that doesn't exist", func() {
			response, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: "does-not-exist",
					Metadata: sharedv1.Metadata_builder{
						Name: "my-name",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.NotFound))
		})

		DescribeTable(
			"Rejects creation with invalid name",
			func(name, expectedError string) {
				response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
					Object: ffv1.Cluster_builder{
						Metadata: sharedv1.Metadata_builder{
							Name: name,
						}.Build(),
						Spec: ffv1.ClusterSpec_builder{
							Template: "my_template",
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				Expect(response).To(BeNil())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(status.Message()).To(ContainSubstring(expectedError))
			},
			Entry(
				"Too long",
				"a234567890123456789012345678901234567890123456789012345678901234",
				"must be at most 63 characters long",
			),
			Entry(
				"Contains uppercase letters",
				"MyCluster",
				"must only contain lowercase letters",
			),
			Entry(
				"Contains special characters",
				"my_cluster",
				"must only contain lowercase letters",
			),
			Entry(
				"Starts with hyphen",
				"-mycluster",
				"cannot start with a hyphen",
			),
			Entry(
				"Ends with hyphen",
				"mycluster-",
				"cannot end with a hyphen",
			),
		)

		It("Rejects update with invalid name", func() {
			// Create the object with a valid name:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Metadata: sharedv1.Metadata_builder{
						Name: "valid-name",
					}.Build(),
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Try to update with an invalid name:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: createResponse.GetObject().GetId(),
					Metadata: sharedv1.Metadata_builder{
						Name: "Invalid_Name",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(updateResponse).To(BeNil())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(status.Message()).To(ContainSubstring("must only contain lowercase letters"))
		})

		DescribeTable(
			"Accepts creation with valid names",
			func(name string) {
				response, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
					Object: ffv1.Cluster_builder{
						Metadata: sharedv1.Metadata_builder{
							Name: name,
						}.Build(),
						Spec: ffv1.ClusterSpec_builder{
							Template: "my_template",
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetObject().GetMetadata().GetName()).To(Equal(name))
			},
			Entry(
				"Simple name",
				"simple",
			),
			Entry(
				"With hyphens",
				"with-hyphens",
			),
			Entry(
				"With numbers",
				"with123numbers",
			),
			Entry(
				"Single character",
				"a",
			),
			Entry(
				"Maximum length",
				"a23456789012345678901234567890123456789012345678901234567890123",
			),
		)

		It("Adds label by updating", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Delete the label:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Metadata: sharedv1.Metadata_builder{
						Labels: map[string]string{
							"example.com/my-label": "my-value",
						},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"metadata.labels",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			labels := object.GetMetadata().GetLabels()
			Expect(labels).To(HaveKeyWithValue("example.com/my-label", "my-value"))

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			labels = object.GetMetadata().GetLabels()
			Expect(labels).To(HaveKeyWithValue("example.com/my-label", "my-value"))
		})

		It("Updates label by updating", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Metadata: sharedv1.Metadata_builder{
						Labels: map[string]string{
							"example.com/my-label": "my-value",
						},
					}.Build(),
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Delete the label:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Metadata: sharedv1.Metadata_builder{
						Labels: map[string]string{
							"example.com/my-label": "your-value",
						},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"metadata.labels",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			labels := object.GetMetadata().GetLabels()
			Expect(labels).To(HaveKeyWithValue("example.com/my-label", "your-value"))

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			labels = object.GetMetadata().GetLabels()
			Expect(labels).To(HaveKeyWithValue("example.com/my-label", "your-value"))
		})

		It("Deletes label by updating", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Metadata: sharedv1.Metadata_builder{
						Labels: map[string]string{
							"example.com/my-label": "my-value",
						},
					}.Build(),
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Delete the label:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Metadata: sharedv1.Metadata_builder{
						Labels: map[string]string{},
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{
						"metadata.labels",
					},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			labels := object.GetMetadata().GetLabels()
			Expect(labels).To(BeEmpty())

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			labels = object.GetMetadata().GetLabels()
			Expect(labels).To(BeEmpty())
		})

		It("Adds label by updating without specifying the field mask", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Delete the label:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Metadata: sharedv1.Metadata_builder{
						Labels: map[string]string{
							"example.com/my-label": "my-value",
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			labels := object.GetMetadata().GetLabels()
			Expect(labels).To(HaveKeyWithValue("example.com/my-label", "my-value"))

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			labels = object.GetMetadata().GetLabels()
			Expect(labels).To(HaveKeyWithValue("example.com/my-label", "my-value"))
		})

		It("Updates label by updating without specifying the field mask", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Metadata: sharedv1.Metadata_builder{
						Labels: map[string]string{
							"example.com/my-label": "my-value",
						},
					}.Build(),
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Delete the label:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Metadata: sharedv1.Metadata_builder{
						Labels: map[string]string{
							"example.com/my-label": "your-value",
						},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			labels := object.GetMetadata().GetLabels()
			Expect(labels).To(HaveKeyWithValue("example.com/my-label", "your-value"))

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			labels = object.GetMetadata().GetLabels()
			Expect(labels).To(HaveKeyWithValue("example.com/my-label", "your-value"))
		})

		It("Updates label by updating without specifying the field mask", func() {
			// Create the object:
			createResponse, err := server.Create(ctx, ffv1.ClustersCreateRequest_builder{
				Object: ffv1.Cluster_builder{
					Metadata: sharedv1.Metadata_builder{
						Labels: map[string]string{
							"example.com/my-label": "my-value",
						},
					}.Build(),
					Spec: ffv1.ClusterSpec_builder{
						Template: "my_template",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Delete the label:
			updateResponse, err := server.Update(ctx, ffv1.ClustersUpdateRequest_builder{
				Object: ffv1.Cluster_builder{
					Id: object.GetId(),
					Metadata: sharedv1.Metadata_builder{
						Labels: map[string]string{},
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()
			labels := object.GetMetadata().GetLabels()
			Expect(labels).To(BeEmpty())

			// Get and verify:
			getResponse, err := server.Get(ctx, ffv1.ClustersGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = getResponse.GetObject()
			labels = object.GetMetadata().GetLabels()
			Expect(labels).To(BeEmpty())
		})
	})
})
