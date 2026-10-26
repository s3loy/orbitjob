package logarchive

import (
	"context"
	"io/fs"
	"os"
	"path"
)

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
