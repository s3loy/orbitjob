package logarchive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
		if dec.Decode(&in) != nil || in.Identity.Validate() != nil {
			http.Error(w, "invalid archive request", http.StatusBadRequest)
			return
		}
		page, err := o.Store.Read(in.Identity, in.Query)
		if err != nil {
			http.Error(w, "archive read unavailable", http.StatusServiceUnavailable)
			return
		}
		if page.State == "unavailable" {
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
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8*MiB))
	if err != nil {
		http.Error(w, "collector batch too large", http.StatusRequestEntityTooLarge)
		return
	}
	sum := sha256.Sum256(body)
	batch := hex.EncodeToString(sum[:])
	decoder := json.NewDecoder(bytes.NewReader(body))
	type pending struct {
		id      Identity
		records []Record
		bytes   int64
		ordinal int
	}
	groups := map[string]*pending{}
	count := 0
	rejected := 0
	flush := func(p *pending) error {
		if len(p.records) == 0 {
			return nil
		}
		err := o.Store.AppendBatch(p.id, fmt.Sprintf("%s:%s:%d", batch, p.id.Key(), p.ordinal), p.records)
		if errors.Is(err, ErrCapacity) || errors.Is(err, ErrSealed) {
			rejected += len(p.records)
			collectedRecords.WithLabelValues("capacity_or_expired").Add(float64(len(p.records)))
		}
		if err == nil {
			collectedRecords.WithLabelValues("accepted").Add(float64(len(p.records)))
		}
		p.records = nil
		p.bytes = 0
		p.ordinal++
		return err
	}
	for {
		var in Input
		if err = decoder.Decode(&in); err == io.EOF {
			break
		} else if err != nil {
			http.Error(w, "invalid collector JSON lines", http.StatusBadRequest)
			return
		}
		count++
		if count > 20000 {
			http.Error(w, "too many collector records", http.StatusRequestEntityTooLarge)
			return
		}
		id, record, e := o.decodeInput(r.Context(), in)
		if e != nil {
			if errors.Is(e, ErrCapacity) {
				rejected++
				collectedRecords.WithLabelValues("capacity_or_expired").Inc()
				continue
			}
			if !errors.Is(e, ErrInvalidSource) {
				http.Error(w, "source verification temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			if id.TenantID != "" {
				if markErr := o.Store.NoteLoss(id, "collector rejected an invalid or unsupported source record"); markErr != nil {
					http.Error(w, "cannot persist collection loss status", http.StatusServiceUnavailable)
					return
				}
			}
			rejected++
			collectedRecords.WithLabelValues("source_unavailable").Inc()
			continue
		}
		key := id.Key()
		p := groups[key]
		if p == nil {
			if len(groups) >= 256 {
				http.Error(w, "too many collector sources", http.StatusRequestEntityTooLarge)
				return
			}
			p = &pending{id: id}
			groups[key] = p
		}
		if p.bytes+int64(len(record.Text)) > 256*1024 || len(p.records) >= 1000 {
			if e = flush(p); e != nil && !errors.Is(e, ErrCapacity) && !errors.Is(e, ErrSealed) {
				http.Error(w, "archive persistence unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		p.records = append(p.records, record)
		p.bytes += int64(len(record.Text))
	}
	for _, p := range groups {
		if e := flush(p); e != nil && !errors.Is(e, ErrCapacity) && !errors.Is(e, ErrSealed) {
			http.Error(w, "archive persistence unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	// Capacity/source failures are explicit, bounded rejection, not an infinite
	// collector retry queue that would fill the node. Metrics/coverage expose loss.
	if rejected > 0 {
		observationErrors.Add(float64(rejected))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"received": count, "rejected": rejected})
}
