package logarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Input struct {
	ID     string `json:"record_id"`
	File   string `json:"source_file"`
	Offset int64  `json:"source_offset"`
	Log    string `json:"log"`
}

// A path locates a candidate only. A persisted or live owner chain must still
// authorize the source; collector-provided names are not identities by themselves.
func inputLocation(file string) (Source, error) {
	invalid := func(reason string) (Source, error) { return Source{}, fmt.Errorf("%w: %s", ErrInvalidSource, reason) }
	if !strings.HasPrefix(file, "/var/log/pods/") || path.Clean(file) != file {
		return invalid("invalid collector file path")
	}
	parts := strings.Split(strings.TrimPrefix(file, "/var/log/pods/"), "/")
	if len(parts) != 3 || parts[1] != "task" || !strings.HasSuffix(parts[2], ".log") {
		return invalid("unsupported collector source")
	}
	pod := strings.Split(parts[0], "_")
	if len(pod) != 3 || pod[0] == "" || pod[1] == "" {
		return invalid("invalid collector Pod path")
	}
	if _, err := uuid.Parse(pod[2]); err != nil {
		return invalid("invalid collector Pod UID")
	}
	restart, err := strconv.ParseInt(strings.TrimSuffix(parts[2], ".log"), 10, 32)
	if err != nil || restart < 0 {
		return invalid("invalid container restart")
	}
	return Source{Namespace: pod[0], PodName: pod[1], PodUID: pod[2], Container: "task", Restart: int32(restart)}, nil
}

// parseInput validates the full wire record without reading or mutating an archive.
func parseInput(in Input) (Source, Record, error) {
	if _, err := uuid.Parse(in.ID); err != nil || in.Offset < 0 || len(in.Log) > MaxRecordBytes+512 {
		return Source{}, Record{}, fmt.Errorf("%w: invalid collector record", ErrInvalidSource)
	}
	loc, err := inputLocation(in.File)
	if err != nil {
		return Source{}, Record{}, err
	}
	cri := strings.SplitN(in.Log, " ", 4)
	if len(cri) != 4 || (cri[1] != "stdout" && cri[1] != "stderr") || (cri[2] != "P" && cri[2] != "F") {
		return Source{}, Record{}, fmt.Errorf("%w: invalid CRI record", ErrInvalidSource)
	}
	at, err := time.Parse(time.RFC3339Nano, cri[0])
	if err != nil {
		return Source{}, Record{}, fmt.Errorf("%w: invalid CRI timestamp", ErrInvalidSource)
	}
	text := cri[3]
	if cri[2] == "F" {
		text += "\n"
	}
	if len(text) > MaxRecordBytes {
		return Source{}, Record{}, fmt.Errorf("%w: CRI record exceeds supported physical line limit", ErrInvalidSource)
	}
	// Timestamp, stream and partial flag are included. Re-read and regrouped
	// chunks retain fingerprints, while file rotation may safely reuse offsets.
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", in.File, in.Offset, in.Log)))
	return loc, Record{ID: hex.EncodeToString(digest[:16]), Kind: cri[1], Stage: "running", Time: at, Text: text}, nil
}

func (s *Store) knownInputIdentity(in Input) (Identity, bool) {
	loc, err := inputLocation(in.File)
	if err != nil {
		return Identity{}, false
	}
	id, _, ok := s.Source(loc.Namespace, loc.PodName, loc.PodUID, loc.Container, loc.Restart)
	return id, ok
}

type resolvedSource struct {
	id     Identity
	source Source
}

func (o *Observer) decodeInputCached(ctx context.Context, in Input, cache map[string]resolvedSource) (Identity, Record, error) {
	loc, record, err := parseInput(in)
	if err != nil {
		return Identity{}, Record{}, err
	}
	resolved, ok := cache[in.File]
	id, source := resolved.id, resolved.source
	if !ok {
		id, source, ok = o.Store.Source(loc.Namespace, loc.PodName, loc.PodUID, loc.Container, loc.Restart)
	}
	if !ok {
		id, source, err = o.resolvePod(ctx, loc.Namespace, loc.PodName, loc.PodUID, loc.Restart, false)
		if err != nil {
			return id, Record{}, err
		}
	}
	if cache != nil {
		cache[in.File] = resolvedSource{id, source}
	}
	record.Attempt, record.PodUID, record.Container, record.Restart = source.Attempt, source.PodUID, source.Container, source.Restart
	return id, record, nil
}
