package logarchive

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
)

// Directory maintenance does not depend on a JobRun still existing. Kubernetes
// retention must not strand a manual cleanup request or an expired directory.
func (o *Observer) maintainDirectory(ctx context.Context, tenant string) error {
	tx, err := tenantTx(ctx, o.Repository.DB, tenant)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT identity_json,cleanup_requested_at IS NOT NULL FROM execution_log_archives
 WHERE tenant_id=$1 AND state NOT IN ('expired','deleted')
 AND (cleanup_requested_at IS NOT NULL OR protected_until<=now())
 ORDER BY protected_until NULLS LAST,archive_id LIMIT 128`, tenant)
	if err != nil {
		return err
	}
	type request struct {
		id     Identity
		manual bool
	}
	var requests []request
	for rows.Next() {
		var data []byte
		var item request
		if err = rows.Scan(&data, &item.manual); err != nil {
			_ = rows.Close()
			return err
		}
		if err = json.Unmarshal(data, &item.id); err != nil {
			_ = rows.Close()
			return err
		}
		requests = append(requests, item)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, item := range requests {
		m, e := o.Store.Snapshot(item.id)
		if errors.Is(e, ErrMissing) {
			// Capacity-only registrations have no payload to delete.
			state, until, statusErr := o.Repository.Status(ctx, item.id)
			if statusErr != nil {
				return statusErr
			}
			if state != "capacity" {
				continue
			}
			m = Manifest{Identity: item.id, State: "expired", ProtectedUntil: until}
			if item.manual {
				m.State = "deleted"
			}
		} else if e != nil {
			return e
		} else if item.manual && m.SealedAt != nil && m.State != "deleted" {
			if e = o.Store.Delete(item.id, true); e != nil {
				return e
			}
			m, e = o.Store.Snapshot(item.id)
			if e != nil {
				return e
			}
		}
		if err = o.Repository.Sync(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// Compact tombstones have a second bounded lifetime; the service is not a
// cumulative execution counter that eventually refuses every future run.
func (s *Store) Reap() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, m := range s.items {
		if (m.State != "expired" && m.State != "deleted") || s.limits.Now().Sub(m.UpdatedAt) < Retention {
			continue
		}
		var size int64
		e := fs.WalkDir(s.root.FS(), key, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fs.ErrInvalid
			}
			size += allocated(info)
			return nil
		})
		if e != nil {
			return e
		}
		info, e := s.root.Stat(path.Join(key, "manifest.json"))
		if e != nil {
			return e
		}
		if e = s.root.RemoveAll(key); e != nil {
			return e
		}
		s.used -= size
		s.metadata -= info.Size()
		delete(s.items, key)
	}
	return nil
}

func (r Repository) Reap(ctx context.Context, tenant string) error {
	tx, e := tenantTx(ctx, r.DB, tenant)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	_, e = tx.ExecContext(ctx, `DELETE FROM execution_log_archives WHERE tenant_id=$1 AND state IN ('expired','deleted') AND protected_until<now()-interval '15 days'`, tenant)
	if e != nil {
		return e
	}
	return tx.Commit()
}
