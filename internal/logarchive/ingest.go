package logarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/google/uuid"
	"path"
	"strconv"
	"strings"
	"time"
)

type Input struct {
	ID     string `json:"record_id"`
	File   string `json:"source_file"`
	Offset int64  `json:"source_offset"`
	Log    string `json:"log"`
}

// CRI file paths locate a candidate only; persisted/live owner checks authorize it.
func (o *Observer) decodeInput(ctx context.Context, in Input) (Identity, Record, error) {
	if _, err := uuid.Parse(in.ID); err != nil || in.Offset < 0 || len(in.Log) > MaxRecordBytes+512 {
		return Identity{}, Record{}, fmt.Errorf("%w: invalid collector record", ErrInvalidSource)
	}
	if !strings.HasPrefix(in.File, "/var/log/pods/") || path.Clean(in.File) != in.File {
		return Identity{}, Record{}, fmt.Errorf("%w: invalid collector file path", ErrInvalidSource)
	}
	parts := strings.Split(strings.TrimPrefix(in.File, "/var/log/pods/"), "/")
	if len(parts) != 3 || parts[1] != "task" {
		return Identity{}, Record{}, fmt.Errorf("%w: unsupported collector source", ErrInvalidSource)
	}
	pod := strings.Split(parts[0], "_")
	if len(pod) != 3 {
		return Identity{}, Record{}, fmt.Errorf("%w: invalid collector Pod path", ErrInvalidSource)
	}
	restart, err := strconv.ParseInt(strings.TrimSuffix(parts[2], ".log"), 10, 32)
	if err != nil || restart < 0 {
		return Identity{}, Record{}, fmt.Errorf("%w: invalid container restart", ErrInvalidSource)
	}
	if _, err = uuid.Parse(pod[2]); err != nil {
		return Identity{}, Record{}, fmt.Errorf("%w: invalid collector Pod UID", ErrInvalidSource)
	}
	id, source, ok := o.Store.Source(pod[0], pod[1], pod[2], parts[1], int32(restart))
	if !ok {
		id, source, err = o.ResolvePod(ctx, pod[0], pod[1], pod[2], int32(restart))
		if err != nil {
			return Identity{}, Record{}, err
		}
	}
	cri := strings.SplitN(in.Log, " ", 4)
	if len(cri) != 4 || (cri[1] != "stdout" && cri[1] != "stderr") || (cri[2] != "P" && cri[2] != "F") {
		return id, Record{}, fmt.Errorf("%w: invalid CRI record", ErrInvalidSource)
	}
	at, err := time.Parse(time.RFC3339Nano, cri[0])
	if err != nil {
		return id, Record{}, fmt.Errorf("%w: invalid CRI timestamp", ErrInvalidSource)
	}
	// Re-reading a retained file after collector Pod recreation must keep the
	// same identity even if chunks are regrouped. Raw CRI includes timestamp,
	// stream and partial flag, so a rotated file can safely reuse its offsets.
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", in.File, in.Offset, in.Log)))
	record := Record{ID: hex.EncodeToString(digest[:16]), Kind: cri[1], Stage: "running", Time: at, Attempt: source.Attempt, PodUID: source.PodUID, Container: source.Container, Restart: source.Restart, Text: cri[3]}
	if cri[2] == "F" {
		record.Text += "\n"
	}
	if len(record.Text) > MaxRecordBytes {
		return id, Record{}, fmt.Errorf("%w: CRI record exceeds supported physical line limit", ErrInvalidSource)
	}
	return id, record, nil
}
