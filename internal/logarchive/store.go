package logarchive

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
)

type Store struct {
	mu       sync.Mutex
	root     *os.Root
	lock     *os.File
	limits   Limits
	items    map[string]*Manifest
	used     int64
	metadata int64
}

func Open(directory string, limits Limits) (*Store, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	s := &Store{root: r, limits: limits, items: make(map[string]*Manifest)}
	s.lock, err = r.OpenFile(".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err == nil {
		err = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("archive writer lock: %w", err)
	}
	err = fs.WalkDir(r.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in archive: %s", name)
		}
		if d.IsDir() {
			info, e := d.Info()
			if e != nil {
				return e
			}
			s.used += allocated(info)
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		s.used += allocated(info)
		if path.Base(name) != "manifest.json" {
			return nil
		}
		if info.Size() > limits.ManifestBytes {
			return fmt.Errorf("oversized archive manifest")
		}
		data, e := r.ReadFile(name)
		if e != nil {
			return e
		}
		var m Manifest
		if e = json.Unmarshal(data, &m); e != nil {
			return fmt.Errorf("read archive manifest: %w", e)
		}
		if e = m.Identity.Validate(); e != nil {
			return e
		}
		if path.Dir(name) != m.Identity.Key() {
			return fmt.Errorf("archive identity/path mismatch")
		}
		s.metadata += int64(len(data))
		if s.metadata > limits.MetadataBytes {
			return ErrCapacity
		}
		s.items[m.Identity.Key()] = &m
		return nil
	})
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	// Remove only unreferenced segments/staging files, after all manifests are known.
	if err = s.reconcile(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func allocated(info os.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return (info.Size() + 4095) / 4096 * 4096
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock != nil {
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		_ = s.lock.Close()
	}
	if s.root != nil {
		return s.root.Close()
	}
	return nil
}

func (s *Store) reconcile() error {
	wanted := map[string]bool{".lock": true}
	for key, m := range s.items {
		wanted[key+"/manifest.json"] = true
		wanted[key+"/control"] = true
		for _, seg := range m.Segments {
			if path.Base(seg.Name) != seg.Name || !strings.HasSuffix(seg.Name, ".gz") {
				return fmt.Errorf("invalid segment path")
			}
			wanted[key+"/"+seg.Name] = true
			if _, err := s.root.Stat(key + "/" + seg.Name); err != nil {
				m.State = "partial"
				m.Reason = "archive segment missing after restart"
			}
		}
	}
	return fs.WalkDir(s.root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || wanted[name] {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if e = s.root.Remove(name); e != nil {
			return e
		}
		s.used -= allocated(info)
		return nil
	})
}

func (s *Store) roomLocked(need int64) bool {
	if need < 0 || s.used+need > s.limits.TotalBytes {
		return false
	}
	var st syscall.Statfs_t
	if syscall.Statfs(s.root.Name(), &st) != nil {
		return false
	}
	available := int64(st.Bavail) * int64(st.Bsize)
	return available-need >= s.limits.ReserveBytes && st.Ffree > 64
}

func (s *Store) atomicLocked(name string, data []byte) error {
	tmp := name + ".tmp"
	f, err := s.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = s.root.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	var old int64
	if info, e := s.root.Stat(name); e == nil {
		old = allocated(info)
	}
	if err = s.root.Rename(tmp, name); err != nil {
		return err
	}
	dir, e := s.root.Open(path.Dir(name))
	if e != nil {
		return e
	}
	err = dir.Sync()
	_ = dir.Close()
	if err != nil {
		return err
	}
	info, err := s.root.Stat(name)
	if err != nil {
		return err
	}
	s.used += allocated(info) - old
	ok = true
	return nil
}

func (s *Store) saveLocked(key string, m *Manifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if int64(len(data)) > s.limits.ManifestBytes {
		return ErrCapacity
	}
	var oldSize int64
	if info, e := s.root.Stat(key + "/manifest.json"); e == nil {
		oldSize = info.Size()
	}
	if s.metadata-oldSize+int64(len(data)) > s.limits.MetadataBytes || !s.roomLocked(int64(len(data))+8192) {
		return ErrCapacity
	}
	if err = s.atomicLocked(key+"/manifest.json", data); err != nil {
		return err
	}
	s.metadata += int64(len(data)) - oldSize
	s.items[key] = m
	return nil
}

func clone(m *Manifest) *Manifest {
	out := *m
	out.Segments = append([]Segment(nil), m.Segments...)
	out.Events = append([]Record(nil), m.Events...)
	out.Gaps = append([]Gap(nil), m.Gaps...)
	out.Seen = append([]string(nil), m.Seen...)
	out.Batches = append([]string(nil), m.Batches...)
	out.Sources = append([]Source(nil), m.Sources...)
	return &out
}

func (s *Store) Bind(id Identity, source Source) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := id.Key()
	old := s.items[key]
	if old == nil {
		return ErrMissing
	}
	if source.Namespace != id.Namespace || source.PodUID == "" || source.JobUID == "" || source.Attempt < 1 || source.Container != "task" {
		return fmt.Errorf("invalid archive source")
	}
	for _, v := range old.Sources {
		if v == source {
			return nil
		}
		if v.PodUID == source.PodUID && (v.JobUID != source.JobUID || v.Attempt != source.Attempt) {
			return fmt.Errorf("archive source identity changed")
		}
	}
	if len(old.Sources) >= 256 {
		return ErrCapacity
	}
	m := clone(old)
	m.Sources = append(m.Sources, source)
	return s.saveLocked(key, m)
}

func (s *Store) Source(namespace, podName, podUID, container string, restart int32) (Identity, Source, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.items {
		for _, v := range m.Sources {
			if v.Namespace == namespace && v.PodName == podName && v.PodUID == podUID && v.Container == container && v.Restart == restart {
				return m.Identity, v, true
			}
		}
	}
	return Identity{}, Source{}, false
}

func (s *Store) Register(id Identity) error {
	if err := id.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := id.Key()
	if m := s.items[key]; m != nil {
		if m.Identity.RunUID != id.RunUID {
			return fmt.Errorf("archive immutable run UID changed")
		}
		if m.Identity.RunID == 0 && id.RunID > 0 {
			next := clone(m)
			next.Identity = id
			return s.saveLocked(key, next)
		}
		return nil
	}
	if !s.roomLocked(s.limits.ManifestBytes * 2) {
		return ErrCapacity
	}
	_, existingDirectory := s.root.Stat(key)
	if err := s.root.MkdirAll(key, 0700); err != nil {
		return err
	}
	if existingDirectory != nil {
		if info, e := s.root.Stat(key); e == nil {
			s.used += allocated(info)
		}
	}
	if err := s.initControlLocked(key); err != nil {
		return err
	}
	now := s.limits.Now().UTC()
	return s.saveLocked(key, &Manifest{Identity: id, State: "collecting", Reason: "collection coverage is being observed", Next: 1, CreatedAt: now, UpdatedAt: now})
}

func (s *Store) Usage() (int64, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.used, s.limits.TotalBytes
}

func (s *Store) Snapshot(id Identity) (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.items[id.Key()]
	if m == nil {
		return Manifest{}, ErrMissing
	}
	out := clone(m)
	s.applyControlLocked(id.Key(), out)
	return *out, nil
}
