package logarchive

import (
	"errors"
	"syscall"
	"testing"
	"time"
)

func TestArchiveRegistrationFilesystemCapacity(t *testing.T) {
	for _, tc := range []struct {
		name          string
		files         uint64
		free          uint64
		lowBytes      bool
		logicalBudget bool
		statfsError   error
		wantCapacity  bool
	}{
		{name: "unreported inode counts"},
		{name: "finite inodes exhausted", files: 1024, wantCapacity: true},
		{name: "finite inodes at reserve", files: 1024, free: 64, wantCapacity: true},
		{name: "finite inodes above reserve", files: 1024, free: 65},
		{name: "insufficient available bytes", lowBytes: true, wantCapacity: true},
		{name: "statfs unavailable", statfsError: errors.New("statfs unavailable"), wantCapacity: true},
		{name: "logical allocation exhausted", logicalBudget: true, wantCapacity: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			limits := testLimits(&now)
			if tc.logicalBudget {
				limits.TotalBytes = 256 * 1024
				limits.RunBytes = 256 * 1024
				limits.HeadBytes = 0
			}
			s, err := Open(t.TempDir(), limits)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			s.statfs = func(_ string, st *syscall.Statfs_t) error {
				if tc.statfsError != nil {
					return tc.statfsError
				}
				st.Bsize = 4096
				st.Bavail = uint64((limits.ReserveBytes + limits.ManifestBytes*2) / int64(st.Bsize))
				if tc.lowBytes {
					st.Bavail--
				}
				st.Files = tc.files
				st.Ffree = tc.free
				return nil
			}
			id := identity(1)
			err = s.Register(id)
			if tc.wantCapacity {
				if !errors.Is(err, ErrCapacity) {
					t.Fatalf("registration did not refuse exhausted capacity: %v", err)
				}
				if _, err = s.Snapshot(id); !errors.Is(err, ErrMissing) {
					t.Fatalf("refused registration retained an archive: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("registration rejected available capacity: %v", err)
			}
			m, err := s.Snapshot(id)
			if err != nil || m.State != "collecting" {
				t.Fatalf("registration did not persist a collecting archive: state=%q error=%v", m.State, err)
			}
		})
	}
}
