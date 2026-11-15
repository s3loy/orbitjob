package logarchive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

type ReadRequest struct {
	Identity Identity `json:"identity"`
	Query    Query    `json:"query"`
}

func (o *Observer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/read", func(w http.ResponseWriter, r *http.Request) {
		if !peer(r, "orbitjob-log-reader") {
			http.Error(w, "internal client identity required", http.StatusForbidden)
			return
		}
		var in ReadRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || dec.Decode(&struct{}{}) != io.EOF || in.Identity.Validate() != nil {
			http.Error(w, "invalid archive request", http.StatusBadRequest)
			return
		}
		page, err := o.Store.ReadContext(r.Context(), in.Identity, in.Query)
		if err != nil {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "archive read unavailable", http.StatusServiceUnavailable)
			return
		}
		if page.State == "unavailable" && len(page.Gaps) == 0 {
			if state, until, e := o.Repository.Status(r.Context(), in.Identity); e == nil {
				switch state {
				case "capacity":
					page.State = state
					page.Reason = "log collection could not reserve capacity; execution continues without a complete archive"
				case "deleted":
					page.State = state
					page.Reason = "logs explicitly deleted by an authorized administrator"
				case "expired":
					page.State = state
					page.Reason = "logs removed after their retention protection interval"
				}
				page.ProtectedUntil = until
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(page)
	})
	mux.HandleFunc("POST /v1/ingest", o.ingestHTTP)
	return mux
}

func peer(r *http.Request, name string) bool {
	return r.TLS != nil && len(r.TLS.VerifiedChains) > 0 && len(r.TLS.PeerCertificates) > 0 && r.TLS.PeerCertificates[0].Subject.CommonName == name
}

func (o *Observer) ingestHTTP(w http.ResponseWriter, r *http.Request) {
	if !peer(r, "orbitjob-log-collector") {
		http.Error(w, "collector identity required", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	release, err := o.Store.coordinate(ctx, "installation-ingest", 2)
	if err != nil {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "archive ingestion busy", http.StatusServiceUnavailable)
		return
	}
	defer release()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8*MiB))
	if err != nil {
		http.Error(w, "collector batch too large", http.StatusRequestEntityTooLarge)
		return
	}
	// Decode and check every structural limit before source registration or
	// appending any output. A malformed tail cannot commit the valid prefix.
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var inputs []Input
	sources := map[string]bool{}
	for {
		var in Input
		if err = decoder.Decode(&in); err == io.EOF {
			break
		} else if err != nil {
			http.Error(w, "invalid collector JSON lines", http.StatusBadRequest)
			return
		}
		inputs = append(inputs, in)
		if len(inputs) > 20000 {
			http.Error(w, "too many collector records", http.StatusRequestEntityTooLarge)
			return
		}
		sources[in.File] = true
		if len(sources) > 256 {
			http.Error(w, "too many collector sources", http.StatusRequestEntityTooLarge)
			return
		}
	}
	type pending struct {
		id      Identity
		records []Record
	}
	groups := map[string]*pending{}
	resolved := map[string]resolvedSource{}
	losses := map[string]Identity{}
	rejected := 0
	for _, in := range inputs {
		if err = ctx.Err(); err != nil {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "collector verification budget exhausted", http.StatusServiceUnavailable)
			return
		}
		id, record, e := o.decodeInputCached(ctx, in, resolved)
		if e != nil {
			switch {
			case errors.Is(e, ErrCapacity), errors.Is(e, ErrSealed), errors.Is(e, ErrQuarantined):
				rejected++
				collectedRecords.WithLabelValues("capacity_expired_or_quarantined").Inc()
				continue
			case errors.Is(e, ErrInvalidSource):
				rejected++
				collectedRecords.WithLabelValues("source_unavailable").Inc()
				if id.TenantID == "" {
					id, _ = o.Store.knownInputIdentity(in)
				}
				if id.TenantID != "" {
					losses[id.Key()] = id
				}
				continue
			default:
				w.Header().Set("Retry-After", "1")
				http.Error(w, "source verification temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		key := id.Key()
		if groups[key] == nil {
			groups[key] = &pending{id: id}
		}
		groups[key].records = append(groups[key].records, record)
	}
	for _, id := range losses {
		if err = o.Store.NoteLoss(id, "collector rejected an invalid or unsupported source record"); err != nil {
			http.Error(w, "cannot persist collection loss status", http.StatusServiceUnavailable)
			return
		}
	}
	sum := sha256.Sum256(body)
	batch := hex.EncodeToString(sum[:])
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		p := groups[key]
		for from, ordinal := 0, 0; from < len(p.records); ordinal++ {
			through, size := from, 0
			for through < len(p.records) && through-from < 1000 {
				n := len(p.records[through].Text)
				if size+n > 256*1024 {
					break
				}
				size += n
				through++
			}
			if err = ctx.Err(); err != nil {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "archive persistence budget exhausted; replay the original batch", http.StatusServiceUnavailable)
				return
			}
			e := o.Store.AppendBatch(p.id, fmt.Sprintf("%s:%s:%d", batch, key, ordinal), p.records[from:through])
			if errors.Is(e, ErrCapacity) || errors.Is(e, ErrSealed) || errors.Is(e, ErrQuarantined) {
				rejected += through - from
				collectedRecords.WithLabelValues("capacity_expired_or_quarantined").Add(float64(through - from))
			} else if e != nil {
				// Earlier groups may already be durable. Replay the entire original
				// batch; source fingerprints deduplicate across batch regrouping.
				w.Header().Set("Retry-After", "1")
				http.Error(w, "archive persistence unavailable; replay the original batch", http.StatusServiceUnavailable)
				return
			} else {
				collectedRecords.WithLabelValues("accepted").Add(float64(through - from))
			}
			from = through
		}
	}
	if rejected > 0 {
		observationErrors.Add(float64(rejected))
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]int{"received": len(inputs), "rejected": rejected})
}
