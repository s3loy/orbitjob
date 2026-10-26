package logarchive

import (
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
