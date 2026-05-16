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

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Private public IPs server", func() {
	var (
		ctx             context.Context
		publicIPPoolDao *dao.GenericDAO[*privatev1.PublicIPPool]
		publicIPDao     *dao.GenericDAO[*privatev1.PublicIP]
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
		tx, err := tm.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := tm.End(ctx, tx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)

		// Create the tables:
		err = dao.CreateTables[*privatev1.PublicIP](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.PublicIPPool](ctx)
		Expect(err).ToNot(HaveOccurred())
		err = dao.CreateTables[*privatev1.ComputeInstance](ctx)
		Expect(err).ToNot(HaveOccurred())

		// Create the PublicIPPool DAO for test data setup:
		publicIPPoolDao, err = dao.NewGenericDAO[*privatev1.PublicIPPool]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create the PublicIP DAO for setting initial state in state machine tests:
		publicIPDao, err = dao.NewGenericDAO[*privatev1.PublicIP]().
			SetLogger(logger).
			SetTenancyLogic(tenancy).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	// createReadyPool creates a PublicIPPool in READY state with the given capacity.
	// Returns the pool ID for use in PublicIP creation.
	createReadyPool := func(ctx context.Context, total int64, allocated int64) string {
		resp, err := publicIPPoolDao.Create().SetObject(
			privatev1.PublicIPPool_builder{
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"shared"},
				}.Build(),
				Spec: privatev1.PublicIPPoolSpec_builder{
					Cidrs: []string{"10.0.0.0/24"},
				}.Build(),
				Status: privatev1.PublicIPPoolStatus_builder{
					State:     privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY,
					Total:     total,
					Allocated: allocated,
					Available: total - allocated,
				}.Build(),
			}.Build(),
		).Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		return resp.GetObject().GetId()
	}

	// createPublicIPInState creates a PublicIP via the server (triggers pool validation and
	// capacity tracking), then sets its state via the DAO (bypasses state machine validation
	// since the controller, not the server, sets the initial state in production).
	createPublicIPInState := func(
		server *PrivatePublicIPsServer,
		state privatev1.PublicIPState,
	) *privatev1.PublicIP {
		poolID := createReadyPool(ctx, 100, 0)
		resp, err := server.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
			Object: privatev1.PublicIP_builder{
				Metadata: privatev1.Metadata_builder{
					Tenants: []string{"shared"},
				}.Build(),
				Spec: privatev1.PublicIPSpec_builder{
					Pool: poolID,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := resp.GetObject()

		object.SetStatus(privatev1.PublicIPStatus_builder{
			State: state,
		}.Build())
		daoResp, err := publicIPDao.Update().SetObject(object).Do(ctx)
		Expect(err).ToNot(HaveOccurred())
		return daoResp.GetObject()
	}

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewPrivatePublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewPrivatePublicIPsServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewPrivatePublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if attribution logic is not set", func() {
			server, err := NewPrivatePublicIPsServer().
				SetLogger(logger).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("attribution logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Validation tests", func() {
		var publicIPsServer *PrivatePublicIPsServer

		BeforeEach(func() {
			var err error

			// Create the server:
			publicIPsServer, err = NewPrivatePublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		Context("Pool required validation", func() {
			It("rejects nil object on Create", func() {
				_, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{}.Build())
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("public IP is mandatory"))
			})

			It("rejects nil spec on Create", func() {
				_, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
					Object: privatev1.PublicIP_builder{
						Metadata: privatev1.Metadata_builder{
							Tenants: []string{"shared"},
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("public IP spec is mandatory"))
			})

			It("rejects empty pool on Create", func() {
				_, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
					Object: privatev1.PublicIP_builder{
						Metadata: privatev1.Metadata_builder{
							Tenants: []string{"shared"},
						}.Build(),
						Spec: privatev1.PublicIPSpec_builder{}.Build(),
					}.Build(),
				}.Build())
				Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				Expect(ok).To(BeTrue())
				Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring("spec.pool"))
			})

			It("accepts valid pool on Create", func() {
				poolID := createReadyPool(ctx, 10, 0)
				response, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
					Object: privatev1.PublicIP_builder{
						Metadata: privatev1.Metadata_builder{
							Tenants: []string{"shared"},
						}.Build(),
						Spec: privatev1.PublicIPSpec_builder{
							Pool: poolID,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.GetObject().GetId()).ToNot(BeEmpty())
			})
		})
	})

	Describe("CRUD operations", func() {
		var (
			publicIPsServer *PrivatePublicIPsServer
			poolID          string
		)

		BeforeEach(func() {
			var err error

			// Create a READY pool for all CRUD tests:
			poolID = createReadyPool(ctx, 100, 0)

			// Create the server:
			publicIPsServer, err = NewPrivatePublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("creates PublicIP with PENDING initial state", func() {
			response, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetObject().GetStatus()).ToNot(BeNil())
			Expect(response.GetObject().GetStatus().GetState()).To(
				Equal(privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING))
		})

		It("creates PublicIP and generates ID", func() {
			response, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			object := response.GetObject()
			Expect(object.GetId()).ToNot(BeEmpty())
			Expect(object.GetMetadata()).ToNot(BeNil())
			Expect(object.GetMetadata().GetCreationTimestamp()).ToNot(BeNil())
		})

		It("retrieves PublicIP by ID", func() {
			createResponse, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			created := createResponse.GetObject()

			getResponse, err := publicIPsServer.Get(ctx, privatev1.PublicIPsGetRequest_builder{
				Id: created.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			retrieved := getResponse.GetObject()
			Expect(proto.Equal(created, retrieved)).To(BeTrue())
		})

		It("lists PublicIPs", func() {
			const count = 3
			for range count {
				_, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
					Object: privatev1.PublicIP_builder{
						Metadata: privatev1.Metadata_builder{
							Tenants: []string{"shared"},
						}.Build(),
						Spec: privatev1.PublicIPSpec_builder{
							Pool: poolID,
						}.Build(),
					}.Build(),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
			}

			response, err := publicIPsServer.List(ctx, privatev1.PublicIPsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetItems()).To(HaveLen(count))
		})

		It("updates PublicIP", func() {
			createResponse, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Name:    "original-name",
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Update the name:
			object.GetMetadata().Name = "updated-name"
			updateResponse, err := publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Verify via Get:
			getResponse, err := publicIPsServer.Get(ctx, privatev1.PublicIPsGetRequest_builder{
				Id: updateResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetName()).To(Equal("updated-name"))
		})

		It("soft deletes PublicIP", func() {
			createResponse, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Finalizers: []string{"test-finalizer"},
						Tenants:    []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()

			// Transition to ALLOCATED (only ALLOCATED PublicIPs can be deleted):
			object.GetStatus().SetState(privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED)
			updateResponse, err := publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object = updateResponse.GetObject()

			// Delete:
			_, err = publicIPsServer.Delete(ctx, privatev1.PublicIPsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Verify soft delete (deletion_timestamp set, object still retrievable):
			getResponse, err := publicIPsServer.Get(ctx, privatev1.PublicIPsGetRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})

		It("signals PublicIP", func() {
			createResponse, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			_, err = publicIPsServer.Signal(ctx, privatev1.PublicIPsSignalRequest_builder{
				Id: createResponse.GetObject().GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("Pool validation on Create", func() {
		var publicIPsServer *PrivatePublicIPsServer

		BeforeEach(func() {
			var err error
			publicIPsServer, err = NewPrivatePublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("rejects Create when pool does not exist", func() {
			_, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: "nonexistent-pool-id",
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("does not exist"))
		})

		It("rejects Create when pool is not READY", func() {
			// Create a pool in PENDING state:
			resp, err := publicIPPoolDao.Create().SetObject(
				privatev1.PublicIPPool_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPPoolSpec_builder{
						Cidrs: []string{"10.0.0.0/24"},
					}.Build(),
					Status: privatev1.PublicIPPoolStatus_builder{
						State:     privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_PENDING,
						Total:     10,
						Allocated: 0,
						Available: 10,
					}.Build(),
				}.Build(),
			).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			pendingPoolID := resp.GetObject().GetId()

			_, err = publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: pendingPoolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(err.Error()).To(ContainSubstring("not in READY state"))
		})

		It("rejects Create when pool has no available capacity", func() {
			exhaustedPoolID := createReadyPool(ctx, 10, 10)

			_, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: exhaustedPoolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(err.Error()).To(ContainSubstring("no available capacity"))
		})

		It("accepts Create when pool is READY with available capacity", func() {
			readyPoolID := createReadyPool(ctx, 10, 0)

			response, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: readyPoolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			Expect(response.GetObject().GetId()).ToNot(BeEmpty())
		})
	})

	Describe("Capacity tracking", func() {
		var publicIPsServer *PrivatePublicIPsServer

		BeforeEach(func() {
			var err error
			publicIPsServer, err = NewPrivatePublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("decrements pool available and increments allocated on Create", func() {
			poolID := createReadyPool(ctx, 10, 2)

			_, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Read the pool and verify capacity was updated:
			poolResp, err := publicIPPoolDao.Get().SetId(poolID).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			pool := poolResp.GetObject()
			Expect(pool.GetStatus().GetAllocated()).To(Equal(int64(3)))
			Expect(pool.GetStatus().GetAvailable()).To(Equal(int64(7)))
		})

		It("increments pool available and decrements allocated on Delete", func() {
			poolID := createReadyPool(ctx, 10, 0)

			// Create a PublicIP (no finalizers, so delete is immediate):
			createResponse, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResponse.GetObject()
			publicIPID := object.GetId()

			// Transition to ALLOCATED (only ALLOCATED PublicIPs can be deleted):
			object.GetStatus().SetState(privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED)
			_, err = publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Verify pool after Create: allocated=1, available=9
			poolResp, err := publicIPPoolDao.Get().SetId(poolID).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(poolResp.GetObject().GetStatus().GetAllocated()).To(Equal(int64(1)))
			Expect(poolResp.GetObject().GetStatus().GetAvailable()).To(Equal(int64(9)))

			// Delete the PublicIP:
			_, err = publicIPsServer.Delete(ctx, privatev1.PublicIPsDeleteRequest_builder{
				Id: publicIPID,
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Verify pool capacity was restored:
			poolResp, err = publicIPPoolDao.Get().SetId(poolID).Do(ctx)
			Expect(err).ToNot(HaveOccurred())
			pool := poolResp.GetObject()
			Expect(pool.GetStatus().GetAllocated()).To(Equal(int64(0)))
			Expect(pool.GetStatus().GetAvailable()).To(Equal(int64(10)))
		})
	})

	Describe("State machine enforcement", func() {
		var publicIPsServer *PrivatePublicIPsServer

		BeforeEach(func() {
			var err error
			publicIPsServer, err = NewPrivatePublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		// setStateOnObject sets the state on a PublicIP, handling the case where Status is nil.
		setStateOnObject := func(object *privatev1.PublicIP, state privatev1.PublicIPState) {
			if object.GetStatus() == nil {
				object.SetStatus(privatev1.PublicIPStatus_builder{
					State: state,
				}.Build())
			} else {
				object.GetStatus().SetState(state)
			}
		}

		// transitionTo updates the PublicIP to the given state via the server's Update method.
		transitionTo := func(object *privatev1.PublicIP, state privatev1.PublicIPState) *privatev1.PublicIP {
			setStateOnObject(object, state)
			resp, err := publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			return resp.GetObject()
		}

		It("accepts PENDING to ALLOCATED transition", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING)
			updated := transitionTo(object, privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED)
			Expect(updated.GetStatus().GetState()).To(Equal(privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED))
		})

		It("accepts ALLOCATED to FAILED transition", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED)
			updated := transitionTo(object, privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED)
			Expect(updated.GetStatus().GetState()).To(Equal(privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED))
		})

		It("accepts PENDING to FAILED transition", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING)
			updated := transitionTo(object, privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED)
			Expect(updated.GetStatus().GetState()).To(Equal(privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED))
		})

		It("accepts FAILED to ALLOCATED transition", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED)
			updated := transitionTo(object, privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED)
			Expect(updated.GetStatus().GetState()).To(Equal(privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED))
		})

		It("rejects FAILED to PENDING transition", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED)
			setStateOnObject(object, privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING)
			_, err := publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(err.Error()).To(ContainSubstring("invalid state transition"))
		})

		It("rejects FAILED to DELETING transition", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED)
			setStateOnObject(object, privatev1.PublicIPState_PUBLIC_IP_STATE_DELETING)
			_, err := publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(err.Error()).To(ContainSubstring("invalid state transition"))
		})

		It("rejects UNSPECIFIED state on full replacement (nil mask)", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED)

			object.GetStatus().SetState(privatev1.PublicIPState_PUBLIC_IP_STATE_UNSPECIFIED)
			_, err := publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(err.Error()).To(ContainSubstring("invalid state transition"))
		})

		It("accepts ALLOCATED to DELETING transition", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED)
			updated := transitionTo(object, privatev1.PublicIPState_PUBLIC_IP_STATE_DELETING)
			Expect(updated.GetStatus().GetState()).To(Equal(privatev1.PublicIPState_PUBLIC_IP_STATE_DELETING))
		})

		It("rejects PENDING to DELETING transition", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING)
			setStateOnObject(object, privatev1.PublicIPState_PUBLIC_IP_STATE_DELETING)
			_, err := publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(err.Error()).To(ContainSubstring("invalid state transition"))
		})

		It("rejects FAILED to DELETING transition", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED)
			setStateOnObject(object, privatev1.PublicIPState_PUBLIC_IP_STATE_DELETING)
			_, err := publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(err.Error()).To(ContainSubstring("invalid state transition"))
		})

		It("skips state validation when UpdateMask excludes status.state", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED)

			object.GetMetadata().Name = "updated-name"
			resp, err := publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"metadata.name"},
				},
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.GetObject().GetMetadata().GetName()).To(Equal("updated-name"))
		})
	})

	Describe("Delete constraint", func() {
		var publicIPsServer *PrivatePublicIPsServer

		BeforeEach(func() {
			var err error
			publicIPsServer, err = NewPrivatePublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("allows Delete when state is ALLOCATED", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED)

			_, err := publicIPsServer.Delete(ctx, privatev1.PublicIPsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("rejects Delete when state is PENDING", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_PENDING)

			_, err := publicIPsServer.Delete(ctx, privatev1.PublicIPsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(err.Error()).To(ContainSubstring("must be in ALLOCATED state"))
		})

		It("rejects Delete when state is FAILED", func() {
			object := createPublicIPInState(publicIPsServer, privatev1.PublicIPState_PUBLIC_IP_STATE_FAILED)

			_, err := publicIPsServer.Delete(ctx, privatev1.PublicIPsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.FailedPrecondition))
			Expect(err.Error()).To(ContainSubstring("must be in ALLOCATED state"))
		})
	})

	Describe("Pool immutability on Update", func() {
		var publicIPsServer *PrivatePublicIPsServer

		BeforeEach(func() {
			var err error
			publicIPsServer, err = NewPrivatePublicIPsServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("rejects Update that changes pool", func() {
			poolA := createReadyPool(ctx, 100, 0)
			poolB := createReadyPool(ctx, 100, 0)

			// Create PublicIP with pool A:
			createResp, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolA,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResp.GetObject()

			// Try to change pool to B:
			object.GetSpec().SetPool(poolB)
			_, err = publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
			}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("spec.pool' is immutable"))
		})

		It("allows Update that keeps same pool", func() {
			poolID := createReadyPool(ctx, 100, 0)

			// Create PublicIP:
			createResp, err := publicIPsServer.Create(ctx, privatev1.PublicIPsCreateRequest_builder{
				Object: privatev1.PublicIP_builder{
					Metadata: privatev1.Metadata_builder{
						Tenants: []string{"shared"},
					}.Build(),
					Spec: privatev1.PublicIPSpec_builder{
						Pool: poolID,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := createResp.GetObject()

			// Update with same pool (change name instead):
			object.GetMetadata().Name = "updated-name"
			updateResp, err := publicIPsServer.Update(ctx, privatev1.PublicIPsUpdateRequest_builder{
				Object: object,
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResp.GetObject().GetMetadata().GetName()).To(Equal("updated-name"))
		})
	})

})
