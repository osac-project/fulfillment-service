package database

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Transaction manager", func() {
	var (
		ctx  context.Context
		ctrl *gomock.Controller
		pool *pgxpool.Pool
	)

	BeforeEach(func() {
		var err error

		ctx = context.Background()

		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)

		db, err := server.NewInstance().Build()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(db.Close)
		pool, err = db.Pool(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)
	})

	Describe("Build", func() {
		It("Should return an error if logger is not set", func() {
			_, err := NewTxManager().
				SetPool(pool).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
		})

		It("Should return an error if pool is not set", func() {
			_, err := NewTxManager().
				SetLogger(logger).
				Build()
			Expect(err).To(MatchError("database connection pool is mandatory"))
		})

		It("Should succeed when all parameters are set", func() {
			manager, err := NewTxManager().
				SetLogger(logger).
				SetPool(pool).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(manager).NotTo(BeNil())
		})
	})

	Describe("Begin transaction", func() {
		var manager TxManager

		BeforeEach(func() {
			var err error
			manager, err = NewTxManager().
				SetLogger(logger).
				SetPool(pool).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should create a new managed transaction", func() {
			tx, err := manager.Begin(ctx)
			Expect(err).To(BeNil())
			Expect(tx).NotTo(BeNil())
		})
	})

	Describe("End transaction", func() {
		var manager TxManager

		BeforeEach(func() {
			var err error
			manager, err = NewTxManager().
				SetLogger(logger).
				SetPool(pool).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should return an error for unsupported transaction types", func() {
			tx := NewMockTx(ctrl)
			err := manager.End(ctx, tx)
			Expect(err).To(MatchError("unsupported transaction type *database.MockTx"))
		})

		It("Should reject transactions created by another manager", func() {
			// Create a transaction with another manager:
			another, err := NewTxManager().
				SetLogger(logger).
				SetPool(pool).
				Build()
			Expect(err).ToNot(HaveOccurred())
			tx, err := another.Begin(ctx)
			Expect(err).ToNot(HaveOccurred())
			defer func() {
				err := another.End(ctx, tx)
				Expect(err).ToNot(HaveOccurred())
			}()

			// Verify that it is rejected:
			err = manager.End(ctx, tx)
			Expect(err).To(MatchError("transaction belongs to another transaction manager"))
		})

		It("Should commit the transaction if no errors are reported", func() {
			// Create a table:
			_, err := pool.Exec(ctx, "create table my_table (my_column text)")
			Expect(err).ToNot(HaveOccurred())

			// Do some changes inside a transaction:
			func() {
				tx, err := manager.Begin(ctx)
				Expect(err).ToNot(HaveOccurred())
				defer func() {
					err := manager.End(ctx, tx)
					Expect(err).ToNot(HaveOccurred())
				}()
				_, err = tx.Exec(ctx, "insert into my_table (my_column) values ($1)", "my_value")
				Expect(err).ToNot(HaveOccurred())
			}()

			// Verify that the changes are still there:
			row := pool.QueryRow(ctx, "select my_column from my_table")
			var value string
			err = row.Scan(&value)
			Expect(err).ToNot(HaveOccurred())
			Expect(value).To(Equal("my_value"))
		})

		It("Should rollback the transaction if errors are reported", func() {
			// Create a table:
			_, err := pool.Exec(ctx, "create table my_table (my_column text)")
			Expect(err).ToNot(HaveOccurred())

			// Do some changes inside a transaction, and report an error:
			func() {
				tx, err := manager.Begin(ctx)
				Expect(err).ToNot(HaveOccurred())
				defer func() {
					err := manager.End(ctx, tx)
					Expect(err).ToNot(HaveOccurred())
				}()
				_, err = tx.Exec(ctx, "insert into my_table (my_column) values ($1)", "my_value")
				Expect(err).ToNot(HaveOccurred())
				err = errors.New("my error")
				tx.ReportError(&err)
			}()

			// Verify that the changes aren't there:
			row := pool.QueryRow(ctx, "select my_column from my_table")
			var value string
			err = row.Scan(&value)
			Expect(err).To(HaveOccurred())
		})
	})
})
