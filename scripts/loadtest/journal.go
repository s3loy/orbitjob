package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Read append-only side-effect observations, not the fixture's dedupe set.
// The generated case IDs identify this run independently of fixture restarts.
func collectFixtureJournal(ctx context.Context, fixtureURL string, cases map[string]bool) ([]LedgerEntry, error) {
	if fixtureURL == "" {
		return nil, fmt.Errorf("fixture URL missing")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(fixtureURL, "/")+"/ledger", nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("journal HTTP status %d", response.StatusCode)
	}
	var observations []struct {
		CaseID    string `json:"case_id"`
		Key       string `json:"idempotency_key"`
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(response.Body).Decode(&observations); err != nil {
		return nil, err
	}
	var entries []LedgerEntry
	for _, observation := range observations {
		if !cases[observation.CaseID] {
			continue
		}
		if observation.Key == "" || observation.Operation == "" {
			return nil, fmt.Errorf("journal observation lacks identity or operation")
		}
		entries = append(entries, LedgerEntry{IdempotencyKey: observation.Key, Operation: observation.Operation})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("empty source for this run's side-effect journal")
	}
	return entries, nil
}
