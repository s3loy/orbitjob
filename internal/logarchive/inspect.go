package logarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"syscall"
)

// Inspection describes stored integrity without printing identities or output.
// Inspect never creates a lock file, removes staging files or rewrites evidence.
// Use a stopped volume or an isolated consistent snapshot.
type Inspection struct {
	Archives        int                `json:"archives"`
	LegacyManifests int                `json:"legacy_manifests"`
	Findings        []IntegrityFinding `json:"findings"`
}

type IntegrityFinding struct {
	Archive   string `json:"archive,omitempty"`
	Component string `json:"component"`
	Reason    string `json:"reason"`
}

func Inspect(ctx context.Context, directory string, limits Limits) (Inspection, error) {
	report := Inspection{Findings: []IntegrityFinding{}}
	if err := limits.Validate(); err != nil {
		return report, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return report, err
	}
	defer func() { _ = root.Close() }()
	if info, e := root.Lstat(".lock"); e == nil && !info.Mode().IsRegular() {
		return report, fmt.Errorf("untrusted archive root: writer lock is not a regular file")
	} else if e != nil && !os.IsNotExist(e) {
		return report, e
	}
	if lock, e := root.Open(".lock"); e == nil {
		defer func() { _ = lock.Close() }()
		if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); e != nil {
			return report, fmt.Errorf("archive writer is active; inspect a stopped volume or an isolated snapshot")
		}
		defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	} else if !os.IsNotExist(e) {
		return report, e
	}
	directories := map[string]bool{}
	manifests := map[string]bool{}
	store := &Store{root: root, limits: limits}
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("untrusted archive root: symlink")
		}
		if d.IsDir() {
			if name != "." {
				if path.Dir(name) != "." || !archiveDirectoryName.MatchString(name) {
					return fmt.Errorf("untrusted archive root: directory layout")
				}
				directories[name] = true
				if len(directories) > 10000 {
					return fmt.Errorf("inspection archive budget exceeded")
				}
			}
			return nil
		}
		if path.Dir(name) == "." && name != ".lock" {
			return fmt.Errorf("untrusted archive root: unexpected file")
		}
		if info, e := d.Info(); e != nil {
			return e
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("untrusted archive root: non-regular file")
		}
		if path.Base(name) != "manifest.json" {
			return nil
		}
		key := path.Dir(name)
		manifests[key] = true
		report.Archives++
		finding := func(component, reason string) {
			report.Findings = append(report.Findings, IntegrityFinding{Archive: key, Component: component, Reason: reason})
		}
		file, e := root.Open(name)
		if e != nil {
			return e
		}
		data, e := io.ReadAll(io.LimitReader(file, limits.ManifestBytes+1))
		_ = file.Close()
		if e != nil {
			return e
		}
		if int64(len(data)) > limits.ManifestBytes {
			finding("manifest", "manifest exceeds limit")
			return nil
		}
		var m Manifest
		if json.Unmarshal(data, &m) != nil || m.Identity.Validate() != nil {
			finding("manifest", "manifest cannot establish an immutable identity")
			return nil
		}
		if m.Identity.Key() != key {
			return fmt.Errorf("untrusted archive root: manifest identity/path mismatch")
		}
		if m.Checksum != "" {
			checksum := m.Checksum
			m.Checksum = ""
			encoded, _ := json.Marshal(m)
			digest := sha256.Sum256(encoded)
			if checksum != hex.EncodeToString(digest[:]) {
				finding("manifest", "checksum does not match")
				return nil
			}
		} else {
			report.LegacyManifests++
		}
		if m.Next < 1 || m.RetainedBytes < 0 || m.OutputBytes < m.RetainedBytes {
			finding("manifest", "invalid manifest counters")
			return nil
		}
		var last int64
		for _, segment := range m.Segments {
			if path.Base(segment.Name) != segment.Name || path.Ext(segment.Name) != ".gz" {
				return fmt.Errorf("untrusted archive root: segment path")
			}
			if segment.First <= last || segment.Last < segment.First || segment.Last >= m.Next || segment.Bytes < 0 || segment.Records < 1 || segment.Records > 1000 {
				finding("manifest", "invalid segment ranges")
				return nil
			}
			last = segment.Last
			if _, e = store.segmentRecords(ctx, key, segment); e != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				finding(segment.Name, "segment is missing or corrupt")
			}
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	for key := range directories {
		if !manifests[key] {
			report.Archives++
			report.Findings = append(report.Findings, IntegrityFinding{Archive: key, Component: "manifest", Reason: "manifest is missing"})
		}
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Archive == report.Findings[j].Archive {
			return report.Findings[i].Component < report.Findings[j].Component
		}
		return report.Findings[i].Archive < report.Findings[j].Archive
	})
	return report, nil
}
