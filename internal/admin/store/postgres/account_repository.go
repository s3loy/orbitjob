package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lib/pq"
	"orbitjob/internal/admin/app/account"
)

// AccountRepository can only invoke the fixed, owner-controlled identity API.
// Global account tables are never granted to the HTTP database role.
type AccountRepository struct {
	db *sql.DB
	tx *sql.Tx
}

func NewAccountRepository(db *sql.DB) *AccountRepository { return &AccountRepository{db: db} }
func (r *AccountRepository) Call(ctx context.Context, operation string, input any, output any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode identity input: %w", err)
	}
	var result []byte
	query := r.db.QueryRowContext
	if r.tx != nil {
		query = r.tx.QueryRowContext
	}
	if err = query(ctx, `SELECT orbitjob_identity($1,$2::jsonb)`, operation, string(raw)).Scan(&result); err != nil {
		var pgerr *pq.Error
		if errors.As(err, &pgerr) {
			switch pgerr.Code {
			case "OJ401":
				return account.ErrUnauthenticated
			case "OJ403":
				return account.ErrForbidden
			case "OJ400", "23514", "22001":
				return account.ErrInvalid
			case "OJ409", "23505", "23503":
				return account.ErrConflict
			case "OJ412":
				return account.ErrLastAdmin
			case "OJ428":
				return account.ErrRecentAuth
			}
		}
		// Database diagnostics can contain SQL parameters. Keep secrets out of error logs.
		if pgerr != nil {
			return fmt.Errorf("identity operation %s failed (SQLSTATE %s)", operation, pgerr.Code)
		}
		return fmt.Errorf("identity operation %s failed", operation)
	}
	if output != nil {
		if err = json.Unmarshal(result, output); err != nil {
			return fmt.Errorf("decode identity result: %w", err)
		}
	}
	return nil
}

// Transaction holds the identity serialization lock through the policy check and
// its write so a concurrent membership revocation cannot race delegation.
func (r *AccountRepository) Transaction(ctx context.Context, fn func(account.Repository) error) error {
	if r.tx != nil {
		return fn(r)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin identity transaction failed")
	}
	defer func() { _ = tx.Rollback() }()
	scoped := &AccountRepository{db: r.db, tx: tx}
	if err = scoped.Call(ctx, "lock", nil, nil); err != nil {
		return err
	}
	if err = fn(scoped); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit identity transaction failed")
	}
	return nil
}
