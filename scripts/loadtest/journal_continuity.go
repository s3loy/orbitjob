package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type JournalCursor struct {
	RunID  string `json:"run_id"`
	BootID string `json:"boot_id"`
	Head   int64  `json:"head"`
}

type JournalEffect struct {
	Sequence   int64               `json:"seq"`
	CaseID     string              `json:"case_id"`
	Operation  string              `json:"operation"`
	Key        string              `json:"idempotency_key"`
	ObservedAt time.Time           `json:"observed_at"`
	Checkpoint *RecoveryCheckpoint `json:"checkpoint,omitempty"`
}

type JournalSnapshot struct {
	Version int             `json:"version"`
	RunID   string          `json:"run_id"`
	BootID  string          `json:"boot_id"`
	Head    int64           `json:"head"`
	Entries []JournalEffect `json:"entries"`
}

func (s JournalSnapshot) Cursor() JournalCursor {
	return JournalCursor{RunID: s.RunID, BootID: s.BootID, Head: s.Head}
}

func readJournalSnapshot(ctx context.Context, base, runID string) (JournalSnapshot, error) {
	var s JournalSnapshot
	if base == "" || runID == "" {
		return s, fmt.Errorf("journal URL or run identity missing")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/journal?run_id="+url.QueryEscape(runID), nil)
	if err != nil {
		return s, err
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return s, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		return s, fmt.Errorf("journal HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 32<<20))
	if err = decoder.Decode(&s); err != nil {
		return s, err
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return s, fmt.Errorf("journal has trailing or oversized data")
	}
	if s.RunID != runID {
		return s, fmt.Errorf("journal belongs to another run")
	}
	if err = validateJournalSnapshot(s); err != nil {
		return s, err
	}
	return s, nil
}

func validateJournalSnapshot(s JournalSnapshot) error {
	if s.Version != 1 || s.RunID == "" || s.BootID == "" || s.Head < 0 || int64(len(s.Entries)) != s.Head {
		return fmt.Errorf("journal version, epoch or sequence range missing")
	}
	for i, e := range s.Entries {
		if e.Sequence != int64(i+1) || e.CaseID == "" || e.Key == "" || e.Operation == "" {
			return fmt.Errorf("journal sequence gap or incomplete effect")
		}
	}
	return nil
}

func verifyJournalContinuity(start *JournalCursor, end JournalSnapshot) error {
	if err := validateJournalSnapshot(end); err != nil {
		return err
	}
	if start == nil || start.RunID == "" || start.BootID == "" || start.Head < 0 || start.RunID != end.RunID || start.BootID != end.BootID || start.Head > end.Head {
		return fmt.Errorf("journal baseline missing, epoch changed or cursor regressed")
	}
	return nil
}

func recoveryEffectCount(s JournalSnapshot, c RecoveryCanary, after JournalCursor) int {
	n := 0
	for _, e := range s.Entries {
		if e.Sequence > after.Head && e.CaseID == c.Definition.CaseID && e.Key == c.Nonce && e.Operation == "recovery" {
			n++
		}
	}
	return n
}
