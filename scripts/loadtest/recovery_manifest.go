package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Recovery definitions are outside the offered workload and its accounting.
type RecoveryManifest struct {
	Version      int              `json:"version"`
	RunID        string           `json:"run_id"`
	ConfigSHA256 string           `json:"config_sha256"`
	Canaries     []RecoveryCanary `json:"canaries"`
}

type RecoveryCanary struct {
	Fault      FaultSpec  `json:"fault"`
	Stage      string     `json:"stage"`
	Nonce      string     `json:"nonce"`
	Definition Definition `json:"definition"`
}

type PreparedCanary struct {
	CaseID     string `json:"case_id"`
	Tenant     string `json:"tenant"`
	Namespace  string `json:"namespace"`
	SourceUID  string `json:"source_uid"`
	RevisionID int64  `json:"revision_id"`
	SLIID      int64  `json:"sli_id,omitempty"`
	SLOID      int64  `json:"slo_id,omitempty"`
}

type PreparedRecovery struct {
	ManifestSHA256 string           `json:"manifest_sha256"`
	Canaries       []PreparedCanary `json:"canaries"`
}

func recoveryDigest(value any) string { return fmt.Sprintf("%x", sha256.Sum256(mustJSON(value))) }

func recoveryDefinition(cfg Config, fault FaultSpec, stage, nonce, image string) Definition {
	name := fmt.Sprintf("recovery-%x", sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%d/%s/%s", cfg.RunID, fault.Name, fault.Offset, stage, nonce))))[:61]
	body := map[string]any{"case_id": name, "operation": "recovery", "idempotency_key": nonce, "hold": stage == "pending"}
	return Definition{CaseID: name, Tenant: cfg.Tenants[0], Category: "recovery", ProductTriggerType: "manual", TriggerOrigin: "manual",
		ScheduledJob: ScheduledJobSpec{Schedule: neverFiresCron, HistorySuccessful: 20, HistoryFailed: 20, TimeoutSeconds: 240, Image: image,
			Command: []string{"curl"}, Args: []string{"--fail-with-body", "--connect-timeout", "5", "--max-time", "210", "-H", "Content-Type: application/json", "--data", string(bytes.TrimSpace(mustJSON(body))), fixtureURL + "/recovery/effect"}},
		Expected: Expected{TerminalState: "success", Attempts: 1, Core: true}}
}

func generateRecoveryManifest(dir string, cfg Config, lock ImageLock) error {
	path := filepath.Join(dir, "recovery.json")
	if _, err := os.Stat(path); err == nil {
		_, err := loadRecoveryManifest(dir, cfg)
		return err
	} else if !os.IsNotExist(err) {
		return err
	}
	image := ""
	for _, i := range lock.Images {
		if i.Name == "curl" {
			image = ImageReference(i)
		}
	}
	if image == "" {
		return fmt.Errorf("recovery canaries require the locked curl image")
	}
	m := RecoveryManifest{Version: 1, RunID: cfg.RunID, ConfigSHA256: configSHA256(cfg)}
	for _, f := range activeFaultPlan(cfg) {
		stages := []string{"fresh"}
		if f.Name == "postgres" {
			stages = []string{"pending", "fresh"}
		}
		for _, stage := range stages {
			nonce := rand.Text()
			m.Canaries = append(m.Canaries, RecoveryCanary{Fault: f, Stage: stage, Nonce: nonce, Definition: recoveryDefinition(cfg, f, stage, nonce, image)})
		}
	}
	return writeStableJSON(path, m)
}

func loadRecoveryManifest(dir string, cfg Config) (RecoveryManifest, error) {
	var m RecoveryManifest
	data, err := os.ReadFile(filepath.Join(dir, "recovery.json"))
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.Version != 1 || m.RunID != cfg.RunID || m.ConfigSHA256 != configSHA256(cfg) {
		return m, fmt.Errorf("recovery manifest does not match this execution")
	}
	i := 0
	seen := map[string]bool{}
	for _, f := range activeFaultPlan(cfg) {
		stages := []string{"fresh"}
		if f.Name == "postgres" {
			stages = []string{"pending", "fresh"}
		}
		for _, stage := range stages {
			if i >= len(m.Canaries) {
				return m, fmt.Errorf("recovery manifest lacks a planned canary")
			}
			c := m.Canaries[i]
			i++
			if c.Fault != f || c.Stage != stage || len(c.Nonce) < 20 || seen[c.Nonce] || len(cfg.Tenants) == 0 {
				return m, fmt.Errorf("invalid recovery canary identity")
			}
			seen[c.Nonce] = true
			want := recoveryDefinition(cfg, f, stage, c.Nonce, c.Definition.ScheduledJob.Image)
			if c.Definition.ScheduledJob.Image == "" || !bytes.Equal(mustJSON(want), mustJSON(c.Definition)) {
				return m, fmt.Errorf("recovery canary definition differs from its declared identity")
			}
		}
	}
	if i != len(m.Canaries) {
		return m, fmt.Errorf("recovery manifest has unplanned canaries")
	}
	return m, nil
}

func loadPreparedRecovery(runDir string, m RecoveryManifest) (PreparedRecovery, error) {
	var p PreparedRecovery
	data, err := os.ReadFile(filepath.Join(runDir, "recovery-prepared.json"))
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if p.ManifestSHA256 != recoveryDigest(m) || len(p.Canaries) != len(m.Canaries) {
		return p, fmt.Errorf("prepared recovery does not match generated manifest")
	}
	seen := map[string]bool{}
	for i, c := range m.Canaries {
		b := p.Canaries[i]
		if b.CaseID != c.Definition.CaseID || b.Tenant != c.Definition.Tenant || b.Namespace != TenantNamespace(b.Tenant) || b.SourceUID == "" || b.RevisionID <= 0 || seen[b.SourceUID] {
			return p, fmt.Errorf("prepared recovery lacks unique complete source identity")
		}
		seen[b.SourceUID] = true
	}
	return p, nil
}

func prepareRecovery(ctx context.Context, runDir string, cfg Config, apiURL string, keys map[string]string) error {
	m, err := loadRecoveryManifest(filepath.Join(runDir, "generated"), cfg)
	if err != nil {
		return err
	}
	p := PreparedRecovery{ManifestSHA256: recoveryDigest(m)}
	// A retry can reuse successfully created monitoring objects after validating
	// their real identities; it never replaces a binding in a started run.
	previous, _ := loadPreparedRecovery(runDir, m)
	for i, c := range m.Canaries {
		objects := scheduledJobManifests([]Definition{c.Definition}, c.Definition.Tenant)
		data, err := yaml.Marshal(objects[0])
		if err != nil {
			return err
		}
		path := filepath.Join(runDir, "cr-manifests", c.Definition.CaseID+".yaml")
		if err = os.WriteFile(path, data, 0600); err != nil {
			return err
		}
		if err = kubectlApply(path); err != nil {
			return err
		}
		if _, err = waitRevisions(ctx, []string{c.Definition.Tenant}, []Definition{c.Definition}); err != nil {
			return err
		}
		var source recoverySource
		if err = readRecoveryObject(ctx, "scheduledjob", TenantNamespace(c.Definition.Tenant), c.Definition.CaseID, &source); err != nil {
			return err
		}
		b := PreparedCanary{CaseID: c.Definition.CaseID, Tenant: c.Definition.Tenant, Namespace: source.Metadata.Namespace, SourceUID: string(source.Metadata.UID), RevisionID: source.Status.ActiveRevision}
		if b.SourceUID == "" || b.RevisionID <= 0 || b.Namespace != TenantNamespace(b.Tenant) {
			return fmt.Errorf("recovery source lacks namespace/UID/revision")
		}
		if c.Fault.Name == "scheduler" {
			client := NewAPIClient(apiURL, keys[b.Tenant])
			if len(previous.Canaries) > i && previous.Canaries[i].SourceUID == b.SourceUID {
				b.SLIID, b.SLOID = previous.Canaries[i].SLIID, previous.Canaries[i].SLOID
			}
			if b.SLIID == 0 {
				b.SLIID, err = createRecoveryMonitor(ctx, client, "slis", map[string]any{"name": b.CaseID, "sli_type": "availability", "source_type": "job_run", "source_config": map[string]string{"source_uid": b.SourceUID}, "aggregation": "ratio", "good_event_criteria": map[string]string{"status": "success"}})
				if err != nil {
					return err
				}
			}
			if b.SLOID == 0 {
				b.SLOID, err = createRecoveryMonitor(ctx, client, "slos", map[string]any{"name": b.CaseID, "sli_id": b.SLIID, "target": 0.99, "window_type": "rolling", "window_duration": "24h"})
				if err != nil {
					return err
				}
			}
		}
		p.Canaries = append(p.Canaries, b)
		// Preserve successful preparation across a later API failure.
		if len(p.Canaries) == len(m.Canaries) {
			if err = writeStableJSON(filepath.Join(runDir, "recovery-prepared.json"), p); err != nil {
				return err
			}
		}
	}
	return writeStableJSON(filepath.Join(runDir, "recovery-prepared.json"), p)
}

func createRecoveryMonitor(ctx context.Context, c *APIClient, kind string, body map[string]any) (int64, error) {
	name := body["name"].(string)
	// Search first: create responses can be lost after committing server state.
	response, err := c.do(ctx, http.MethodGet, "/api/v1/"+kind+"?limit=100&search="+url.QueryEscape(name), "", nil)
	if err != nil {
		return 0, err
	}
	var list struct {
		Items []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&list)
	_ = response.Body.Close()
	if response.StatusCode != 200 || decodeErr != nil {
		return 0, fmt.Errorf("read recovery %s: HTTP %d", kind, response.StatusCode)
	}
	var found int64
	for _, item := range list.Items {
		if item.Name == name {
			if found != 0 {
				return 0, fmt.Errorf("ambiguous recovery monitor")
			}
			found = item.ID
		}
	}
	if found > 0 {
		return found, nil
	} // SQL verification checks configuration and ownership.
	response, err = c.do(ctx, http.MethodPost, "/api/v1/"+kind, "", bytes.NewReader(mustJSON(body)))
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	var out struct {
		ID int64 `json:"id"`
	}
	if response.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("create recovery %s: HTTP %d", kind, response.StatusCode)
	}
	if err = json.NewDecoder(response.Body).Decode(&out); err != nil || out.ID <= 0 {
		return 0, fmt.Errorf("recovery %s response lacks identity", kind)
	}
	return out.ID, nil
}
