/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package database

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TxManager is a database transaction manager. It knows how to start transactions.
//
//go:generate mockgen -destination=database_tx_manager_mock.go -package=database . TxManager
type TxManager interface {
	// Begin starts a new transaction.
	Begin(ctx context.Context) (Tx, error)
}

// TxManagerBuilder is a builder responsible for constructing database transaction managers. Don't create instances of
// this type directly, use the NewTxManager function instead.
type TxManagerBuilder struct {
	logger *slog.Logger
	pool   *pgxpool.Pool
}

// txManager is responsible for managing database transactions. It provides functionality to interact with a PostgreSQL
// connection pool and logs transaction-related operations using the provided logger.
type txManager struct {
	logger *slog.Logger
	pool   *pgxpool.Pool
}

// NewTxManager creates a builder that can then be used to initializa a new transaction manager.
func NewTxManager() *TxManagerBuilder {
	return &TxManagerBuilder{}
}

// SetLogger sets the logger that the transaction manager will use to write to the log. This is mandatory.
func (b *TxManagerBuilder) SetLogger(value *slog.Logger) *TxManagerBuilder {
	b.logger = value
	return b
}

// SetPool sets the database connection pool that the transaction manager will use to create transactions. This is
// mandatory.
func (b *TxManagerBuilder) SetPool(value *pgxpool.Pool) *TxManagerBuilder {
	b.pool = value
	return b
}

// Build uses the information stored in the builder to create a new transaction manager.
func (b *TxManagerBuilder) Build() (result TxManager, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.pool == nil {
		err = errors.New("database connection pool is mandatory")
		return
	}

	// Create and populate the object:
	result = &txManager{
		logger: b.logger,
		pool:   b.pool,
	}
	return
}

// Begin starts a new transaction. Note that the created transaction is lazy in the sense that it will not create a real
// database transaction till one of the Query or Exec methods is called.
func (m *txManager) Begin(ctx context.Context) (tx Tx, err error) {
	tx = &managedTx{
		manager: m,
	}
	return
}

func (t *managedTx) End(ctx context.Context) error {
	if t.real == nil {
		return nil
	}
	if len(t.errs) == 0 {
		t.manager.logger.DebugContext(ctx, "Committing transaction")
		return t.real.Commit(ctx)
	}
	t.manager.logger.DebugContext(
		ctx,
		"Rolling back transaction",
		slog.Any("errors", t.errs),
	)
	return t.real.Rollback(ctx)
}

// managedTx is an implementation of the transaction interface that will start a real transaction only when one of the
// methods of the interface that require it is called. This is intended to avoid the cost of real transactions for code
// that doesn't interact with the database.
type managedTx struct {
	manager *txManager
	real    pgx.Tx
	errs    []error
}

func (t *managedTx) Query(ctx context.Context, query string, args ...any) (result pgx.Rows, err error) {
	err = t.ensureReal(ctx)
	if err != nil {
		return
	}
	result, err = t.real.Query(ctx, query, args...)
	return
}

func (t *managedTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	err := t.ensureReal(ctx)
	if err != nil {
		return &managedRow{
			err: err,
		}
	}
	return t.real.QueryRow(ctx, query, args...)
}

func (t *managedTx) Exec(ctx context.Context, query string, args ...any) (tag pgconn.CommandTag, err error) {
	err = t.ensureReal(ctx)
	if err != nil {
		return
	}
	tag, err = t.real.Exec(ctx, query, args...)
	return
}

func (t *managedTx) ReportError(err *error) {
	if err != nil && *err != nil {
		t.errs = append(t.errs, *err)
	}
}

// ensureReal makes sure that the real transaction exists, creating it if needed.
func (t *managedTx) ensureReal(ctx context.Context) error {
	if t.real != nil {
		return nil
	}
	t.manager.logger.DebugContext(ctx, "Starting transaction")
	var err error
	t.real, err = t.manager.pool.Begin(ctx)
	return err
}

// managedRow is an implementation of the row interface that always returns the contained error. This is necessary
// because we start transactions lazyly when the QueryRow method is called, and there is no way to return errors
// directly from that. Instead we need to save the error and return it later, when the Scan method is called.
type managedRow struct {
	err error
}

func (r *managedRow) Scan(dest ...any) error {
	return r.err
}
