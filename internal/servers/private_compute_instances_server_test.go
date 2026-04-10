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

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Private compute instances server", func() {
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
		err = dao.CreateTables[*privatev1.ComputeInstanceTemplate](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.ComputeInstance](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.Subnet](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.SecurityGroup](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.VirtualNetwork](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.NetworkClass](ctx)
		Expect(err).ToNot(HaveOccurred())
	})

	// Helper function to create a NetworkClass for test setup
	createTestNetworkClass := func(ctx context.Context) *privatev1.NetworkClass {
		ncDao, err := dao.NewGenericDAO[*privatev1.NetworkClass]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		nc := privatev1.NetworkClass_builder{
			ImplementationStrategy: "test-strategy",
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"shared"},
			}.Build(),
			Capabilities: privatev1.NetworkClassCapabilities_builder{
				SupportsIpv4:      true,
				SupportsIpv6:      true,
				SupportsDualStack: true,
			}.Build(),
			Status: privatev1.NetworkClassStatus_builder{
				State: privatev1.NetworkClassState_NETWORK_CLASS_STATE_READY,
			}.Build(),
		}.Build()

		response, err := ncDao.Create().SetObject(nc).Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		return response.GetObject()
	}

	// Helper function to create a VirtualNetwork for test setup
	createTestVirtualNetwork := func(ctx context.Context, networkClassID string) *privatev1.VirtualNetwork {
		vnDao, err := dao.NewGenericDAO[*privatev1.VirtualNetwork]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		vn := privatev1.VirtualNetwork_builder{
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"shared"},
			}.Build(),
			Spec: privatev1.VirtualNetworkSpec_builder{
				Ipv4Cidr:     proto.String("10.0.0.0/16"),
				NetworkClass: networkClassID,
			}.Build(),
			Status: privatev1.VirtualNetworkStatus_builder{
				State: privatev1.VirtualNetworkState_VIRTUAL_NETWORK_STATE_READY,
			}.Build(),
		}.Build()

		response, err := vnDao.Create().SetObject(vn).Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		return response.GetObject()
	}

	// Helper function to create a Subnet with specified state
	createTestSubnet := func(ctx context.Context, vnID string, state privatev1.SubnetState) *privatev1.Subnet {
		subnetDao, err := dao.NewGenericDAO[*privatev1.Subnet]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		subnet := privatev1.Subnet_builder{
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"shared"},
			}.Build(),
			Spec: privatev1.SubnetSpec_builder{
				VirtualNetwork: vnID,
				Ipv4Cidr:       proto.String("10.0.1.0/24"),
			}.Build(),
			Status: privatev1.SubnetStatus_builder{
				State: state,
			}.Build(),
		}.Build()

		response, err := subnetDao.Create().SetObject(subnet).Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		return response.GetObject()
	}

	// Helper function to create a SecurityGroup with specified state
	createTestSecurityGroup := func(ctx context.Context, vnID string, state privatev1.SecurityGroupState) *privatev1.SecurityGroup {
		sgDao, err := dao.NewGenericDAO[*privatev1.SecurityGroup]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		sg := privatev1.SecurityGroup_builder{
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"shared"},
			}.Build(),
			Spec: privatev1.SecurityGroupSpec_builder{
				VirtualNetwork: vnID,
			}.Build(),
			Status: privatev1.SecurityGroupStatus_builder{
				State: state,
			}.Build(),
		}.Build()

		response, err := sgDao.Create().SetObject(sg).Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		return response.GetObject()
	}

	Describe("Builder", func() {
		It("Creates server with logger", func() {
			server, err := NewPrivateComputeInstancesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Doesn't create server without logger", func() {
			server, err := NewPrivateComputeInstancesServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPrivateComputeInstancesServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivateComputeInstancesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var server *PrivateComputeInstancesServer

		BeforeEach(func() {
			var err error

			// Create the server:
			server, err = NewPrivateComputeInstancesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		// Helper function to create a template
		createTemplate := func(templateID string) {
			// Create a template DAO to insert a template
			templatesDao, err := dao.NewGenericDAO[*privatev1.ComputeInstanceTemplate]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create default values for parameters
			cpuDefault, err := anypb.New(wrapperspb.Int32(1))
			Expect(err).ToNot(HaveOccurred())
			memoryDefault, err := anypb.New(wrapperspb.Int32(2))
			Expect(err).ToNot(HaveOccurred())

			template := privatev1.ComputeInstanceTemplate_builder{
				Id:          templateID,
				Title:       "Test Template",
				Description: "Test template for validation",
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"shared"},
				}.Build(),
				Parameters: []*privatev1.ComputeInstanceTemplateParameterDefinition{
					{
						Name:        "cpu_count",
						Title:       "CPU Count",
						Description: "Number of CPU cores",
						Required:    false,
						Type:        "type.googleapis.com/google.protobuf.Int32Value",
						Default:     cpuDefault,
					},
					{
						Name:        "memory_gb",
						Title:       "Memory (GB)",
						Description: "Amount of memory in GB",
						Required:    false,
						Type:        "type.googleapis.com/google.protobuf.Int32Value",
						Default:     memoryDefault,
					},
				},
			}.Build()

			_, err = templatesDao.Create().SetObject(template).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
		}

		It("Creates object", func() {
			// Create a template first
			createTemplate("general.small")

			// Create template parameters
			templateParams := make(map[string]*anypb.Any)
			cpuParam, err := anypb.New(wrapperspb.Int32(2))
			Expect(err).ToNot(HaveOccurred())
			templateParams["cpu_count"] = cpuParam

			memoryParam, err := anypb.New(wrapperspb.Int32(4))
			Expect(err).ToNot(HaveOccurred())
			templateParams["memory_gb"] = memoryParam

			response, err := server.Create(ctx, privatev1.ComputeInstancesCreateRequest_builder{
				Object: privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template:           "general.small",
						TemplateParameters: templateParams,
					}.Build(),
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).ToNot(BeEmpty())
			Expect(object.GetSpec().GetTemplate()).To(Equal("general.small"))
			Expect(object.GetStatus().GetState()).To(Equal(privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING))
		})

		It("List objects", func() {
			// Create templates and objects:
			const count = 10
			for i := range count {
				templateID := fmt.Sprintf("template-%d", i)
				createTemplate(templateID)

				_, err := server.Create(ctx, privatev1.ComputeInstancesCreateRequest_builder{
					Object: privatev1.ComputeInstance_builder{
						Spec: privatev1.ComputeInstanceSpec_builder{
							Template: templateID,
						}.Build(),
						Status: privatev1.ComputeInstanceStatus_builder{
							State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects:
			response, err := server.List(ctx, privatev1.ComputeInstancesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			// Create templates and objects:
			const count = 10
			for i := range count {
				templateID := fmt.Sprintf("template-limit-%d", i)
				createTemplate(templateID)

				_, err := server.Create(ctx, privatev1.ComputeInstancesCreateRequest_builder{
					Object: privatev1.ComputeInstance_builder{
						Spec: privatev1.ComputeInstanceSpec_builder{
							Template: templateID,
						}.Build(),
						Status: privatev1.ComputeInstanceStatus_builder{
							State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects with limit:
			response, err := server.List(ctx, privatev1.ComputeInstancesListRequest_builder{
				Limit: proto.Int32(5),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(5))
		})

		It("List objects with offset", func() {
			// Create templates and objects:
			const count = 10
			for i := range count {
				templateID := fmt.Sprintf("template-offset-%d", i)
				createTemplate(templateID)

				_, err := server.Create(ctx, privatev1.ComputeInstancesCreateRequest_builder{
					Object: privatev1.ComputeInstance_builder{
						Spec: privatev1.ComputeInstanceSpec_builder{
							Template: templateID,
						}.Build(),
						Status: privatev1.ComputeInstanceStatus_builder{
							State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			// List the objects with offset:
			response, err := server.List(ctx, privatev1.ComputeInstancesListRequest_builder{
				Offset: proto.Int32(5),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(5))
		})

		It("Gets object", func() {
			// Create a template first
			createTemplate("general.small")

			// Create an object:
			createResponse, err := server.Create(ctx, privatev1.ComputeInstancesCreateRequest_builder{
				Object: privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: "general.small",
					}.Build(),
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(createResponse).ToNot(BeNil())
			createdObject := createResponse.GetObject()
			Expect(createdObject).ToNot(BeNil())
			id := createdObject.GetId()
			Expect(id).ToNot(BeEmpty())

			// Get the object:
			getResponse, err := server.Get(ctx, privatev1.ComputeInstancesGetRequest_builder{
				Id: id,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse).ToNot(BeNil())
			object := getResponse.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).To(Equal(id))
			Expect(object.GetSpec().GetTemplate()).To(Equal("general.small"))
			Expect(object.GetStatus().GetState()).To(Equal(privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING))
		})

		It("Updates object", func() {
			// Create templates first
			createTemplate("general.small")
			createTemplate("general.large")

			// Create an object:
			createResponse, err := server.Create(ctx, privatev1.ComputeInstancesCreateRequest_builder{
				Object: privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: "general.small",
					}.Build(),
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(createResponse).ToNot(BeNil())
			createdObject := createResponse.GetObject()
			Expect(createdObject).ToNot(BeNil())
			id := createdObject.GetId()
			Expect(id).ToNot(BeEmpty())

			// Update the object:
			updateResponse, err := server.Update(ctx, privatev1.ComputeInstancesUpdateRequest_builder{
				Object: privatev1.ComputeInstance_builder{
					Id: id,
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: "general.large",
					}.Build(),
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.template", "status.state"},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse).ToNot(BeNil())
			object := updateResponse.GetObject()
			Expect(object).ToNot(BeNil())
			Expect(object.GetId()).To(Equal(id))
			Expect(object.GetSpec().GetTemplate()).To(Equal("general.large"))
			Expect(object.GetStatus().GetState()).To(Equal(privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING))
		})

		It("Deletes object", func() {
			// Create a template first
			createTemplate("general.small")

			// Create an object:
			createResponse, err := server.Create(ctx, privatev1.ComputeInstancesCreateRequest_builder{
				Object: privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: "general.small",
					}.Build(),
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(createResponse).ToNot(BeNil())
			createdObject := createResponse.GetObject()
			Expect(createdObject).ToNot(BeNil())
			id := createdObject.GetId()
			Expect(id).ToNot(BeEmpty())

			// Delete the object:
			deleteResponse, err := server.Delete(ctx, privatev1.ComputeInstancesDeleteRequest_builder{
				Id: id,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(deleteResponse).ToNot(BeNil())

			// Verify the object is deleted:
			getResponse, err := server.Get(ctx, privatev1.ComputeInstancesGetRequest_builder{
				Id: id,
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(getResponse).To(BeNil())
		})

		It("Handles non-existent object", func() {
			// Try to get a non-existent object:
			getResponse, err := server.Get(ctx, privatev1.ComputeInstancesGetRequest_builder{
				Id: "non-existent-id",
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(getResponse).To(BeNil())
		})

		It("Handles empty object in create request", func() {
			// Try to create with nil object:
			response, err := server.Create(ctx, privatev1.ComputeInstancesCreateRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
		})

		It("Handles empty object in update request", func() {
			// Try to update with nil object:
			response, err := server.Update(ctx, privatev1.ComputeInstancesUpdateRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
		})

		It("Handles empty ID in get request", func() {
			// Try to get with empty ID:
			response, err := server.Get(ctx, privatev1.ComputeInstancesGetRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
		})

		It("Handles empty ID in delete request", func() {
			// Try to delete with empty ID:
			response, err := server.Delete(ctx, privatev1.ComputeInstancesDeleteRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
		})

		It("Validates template exists on create", func() {
			// Try to create with non-existent template:
			response, err := server.Create(ctx, privatev1.ComputeInstancesCreateRequest_builder{
				Object: privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: "non-existent-template",
					}.Build(),
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
		})

		It("Validates template exists on update", func() {
			// Create a template and compute instance first:
			createTemplate("existing-template")

			createResponse, err := server.Create(ctx, privatev1.ComputeInstancesCreateRequest_builder{
				Object: privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: "existing-template",
					}.Build(),
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(createResponse).ToNot(BeNil())

			id := createResponse.GetObject().GetId()

			// Try to update with non-existent template:
			updateResponse, err := server.Update(ctx, privatev1.ComputeInstancesUpdateRequest_builder{
				Object: privatev1.ComputeInstance_builder{
					Id: id,
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: "non-existent-template",
					}.Build(),
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
					}.Build(),
				}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"spec.template"},
				},
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(updateResponse).To(BeNil())
		})

		It("Validates template ID is not empty", func() {
			// Try to create with empty template ID:
			response, err := server.Create(ctx, privatev1.ComputeInstancesCreateRequest_builder{
				Object: privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: "",
					}.Build(),
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
		})
	})

	Describe("Network validation", func() {
		var (
			server         *PrivateComputeInstancesServer
			template       *privatev1.ComputeInstanceTemplate
			networkClass   *privatev1.NetworkClass
			virtualNetwork *privatev1.VirtualNetwork
		)

		BeforeEach(func() {
			var err error

			// Create network resources
			networkClass = createTestNetworkClass(ctx)
			virtualNetwork = createTestVirtualNetwork(ctx, networkClass.GetId())

			// Create test template
			templatesDao, err := dao.NewGenericDAO[*privatev1.ComputeInstanceTemplate]().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			cpuDefault, err := anypb.New(wrapperspb.Int32(1))
			Expect(err).ToNot(HaveOccurred())
			memoryDefault, err := anypb.New(wrapperspb.Int32(2))
			Expect(err).ToNot(HaveOccurred())

			template = privatev1.ComputeInstanceTemplate_builder{
				Id:          "test-template",
				Title:       "Test Template",
				Description: "Test template for network validation",
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"shared"},
				}.Build(),
				Parameters: []*privatev1.ComputeInstanceTemplateParameterDefinition{
					{
						Name:        "cpu_count",
						Title:       "CPU Count",
						Description: "Number of CPU cores",
						Required:    false,
						Type:        "type.googleapis.com/google.protobuf.Int32Value",
						Default:     cpuDefault,
					},
					{
						Name:        "memory_gb",
						Title:       "Memory (GB)",
						Description: "Amount of memory in GB",
						Required:    false,
						Type:        "type.googleapis.com/google.protobuf.Int32Value",
						Default:     memoryDefault,
					},
				},
			}.Build()

			_, err = templatesDao.Create().SetObject(template).Do(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Create the server:
			server, err = NewPrivateComputeInstancesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		Context("Subnet validation", func() {
			It("Should succeed with valid READY Subnet", func() {
				subnet := createTestSubnet(ctx, virtualNetwork.GetId(), privatev1.SubnetState_SUBNET_STATE_READY)

				vm := privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: template.GetId(),
						Subnet:   proto.String(subnet.GetId()),
					}.Build(),
				}.Build()

				request := &privatev1.ComputeInstancesCreateRequest{}
				request.SetObject(vm)

				response, err := server.Create(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.GetObject().GetSpec().GetSubnet()).To(Equal(subnet.GetId()))
			})

			It("Should fail with non-existent Subnet", func() {
				vm := privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: template.GetId(),
						Subnet:   proto.String("non-existent-subnet-id"),
					}.Build(),
				}.Build()

				request := &privatev1.ComputeInstancesCreateRequest{}
				request.SetObject(vm)

				response, err := server.Create(ctx, request)
				Expect(err).To(HaveOccurred())
				Expect(response).To(BeNil())

				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(status.Message()).To(ContainSubstring("subnet"))
				Expect(status.Message()).To(ContainSubstring("does not exist"))
			})

			It("Should fail with Subnet not in READY state", func() {
				subnet := createTestSubnet(ctx, virtualNetwork.GetId(), privatev1.SubnetState_SUBNET_STATE_PENDING)

				vm := privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: template.GetId(),
						Subnet:   proto.String(subnet.GetId()),
					}.Build(),
				}.Build()

				request := &privatev1.ComputeInstancesCreateRequest{}
				request.SetObject(vm)

				response, err := server.Create(ctx, request)
				Expect(err).To(HaveOccurred())
				Expect(response).To(BeNil())

				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
				Expect(status.Message()).To(ContainSubstring("not in READY state"))
			})
		})

		Context("SecurityGroup validation", func() {
			var subnet *privatev1.Subnet

			BeforeEach(func() {
				subnet = createTestSubnet(ctx, virtualNetwork.GetId(), privatev1.SubnetState_SUBNET_STATE_READY)
			})

			It("Should succeed with valid READY SecurityGroups", func() {
				sg1 := createTestSecurityGroup(ctx, virtualNetwork.GetId(), privatev1.SecurityGroupState_SECURITY_GROUP_STATE_READY)
				sg2 := createTestSecurityGroup(ctx, virtualNetwork.GetId(), privatev1.SecurityGroupState_SECURITY_GROUP_STATE_READY)

				vm := privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template:       template.GetId(),
						Subnet:         proto.String(subnet.GetId()),
						SecurityGroups: []string{sg1.GetId(), sg2.GetId()},
					}.Build(),
				}.Build()

				request := &privatev1.ComputeInstancesCreateRequest{}
				request.SetObject(vm)

				response, err := server.Create(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.GetObject().GetSpec().GetSecurityGroups()).To(Equal([]string{sg1.GetId(), sg2.GetId()}))
			})

			It("Should fail with non-existent SecurityGroup", func() {
				vm := privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template:       template.GetId(),
						Subnet:         proto.String(subnet.GetId()),
						SecurityGroups: []string{"non-existent-sg-id"},
					}.Build(),
				}.Build()

				request := &privatev1.ComputeInstancesCreateRequest{}
				request.SetObject(vm)

				response, err := server.Create(ctx, request)
				Expect(err).To(HaveOccurred())
				Expect(response).To(BeNil())

				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(status.Message()).To(ContainSubstring("security group"))
				Expect(status.Message()).To(ContainSubstring("does not exist"))
			})

			It("Should fail with SecurityGroup not in READY state", func() {
				sg := createTestSecurityGroup(ctx, virtualNetwork.GetId(), privatev1.SecurityGroupState_SECURITY_GROUP_STATE_PENDING)

				vm := privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template:       template.GetId(),
						Subnet:         proto.String(subnet.GetId()),
						SecurityGroups: []string{sg.GetId()},
					}.Build(),
				}.Build()

				request := &privatev1.ComputeInstancesCreateRequest{}
				request.SetObject(vm)

				response, err := server.Create(ctx, request)
				Expect(err).To(HaveOccurred())
				Expect(response).To(BeNil())

				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
				Expect(status.Message()).To(ContainSubstring("security group"))
				Expect(status.Message()).To(ContainSubstring("not in READY state"))
			})

			It("Should fail with SecurityGroup from different VirtualNetwork", func() {
				// Create another VirtualNetwork
				otherVN := createTestVirtualNetwork(ctx, networkClass.GetId())
				sgFromOtherVN := createTestSecurityGroup(ctx, otherVN.GetId(), privatev1.SecurityGroupState_SECURITY_GROUP_STATE_READY)

				vm := privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template:       template.GetId(),
						Subnet:         proto.String(subnet.GetId()),
						SecurityGroups: []string{sgFromOtherVN.GetId()},
					}.Build(),
				}.Build()

				request := &privatev1.ComputeInstancesCreateRequest{}
				request.SetObject(vm)

				response, err := server.Create(ctx, request)
				Expect(err).To(HaveOccurred())
				Expect(response).To(BeNil())

				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(status.Message()).To(ContainSubstring("VirtualNetwork"))
				Expect(status.Message()).To(ContainSubstring(virtualNetwork.GetId()))
				Expect(status.Message()).To(ContainSubstring(otherVN.GetId()))
			})
		})

		Context("Optional network fields", func() {
			It("Should succeed with no network references", func() {
				vm := privatev1.ComputeInstance_builder{
					Spec: privatev1.ComputeInstanceSpec_builder{
						Template: template.GetId(),
					}.Build(),
				}.Build()

				request := &privatev1.ComputeInstancesCreateRequest{}
				request.SetObject(vm)

				response, err := server.Create(ctx, request)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.GetObject().GetSpec().GetSubnet()).To(BeEmpty())
				Expect(response.GetObject().GetSpec().GetSecurityGroups()).To(BeEmpty())
			})
		})
	})
})
