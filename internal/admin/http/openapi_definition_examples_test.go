package http

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"orbitjob/internal/admin/definition"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/scan"
)

type exampleDefinitionBackend struct {
	definition.Backend
	tenant string
	calls  int
}

func (b *exampleDefinitionBackend) Tenant(string) (string, bool) { return b.tenant, true }
func (b *exampleDefinitionBackend) Apply(_ context.Context, kind, namespace, name string, request definition.ApplyRequest) (definition.Result, error) {
	b.calls++
	spec, digest, err := definition.CanonicalSpec(kind, request.SpecJSON)
	return definition.Result{Kind: kind, Namespace: namespace, Name: name, RequestID: request.RequestID, SpecJSON: spec, ManifestDigest: digest, State: "validated"}, err
}

type exampleDefinitionStore struct {
	definition.OperationStore
	registered *definition.Operation
}

func (s *exampleDefinitionStore) Register(_ context.Context, _ definition.Subject, operation definition.Operation) error {
	s.registered = &operation
	return nil
}

type exampleImageResolver struct{}

func (exampleImageResolver) Pin(context.Context, string) (string, error) {
	return "busybox@sha256:" + strings.Repeat("a", 64), nil
}

func TestOpenAPIDefinitionExamplesPassStandardCreationService(t *testing.T) {
	document := ServiceOpenAPIDocument()
	for _, kind := range []string{"jobs", "workflows"} {
		t.Run(kind, func(t *testing.T) {
			operation := document.Paths[adminAPIPrefix+"/definitions/"+kind+"/{namespace}/{name}"].Put
			raw, err := json.Marshal(operation.RequestBody.Content["application/json"].Example)
			if err != nil {
				t.Fatal(err)
			}
			var request definition.ApplyRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				t.Fatal(err)
			}
			if !request.DryRun || request.ExpectedUID != "" {
				t.Fatalf("expected a new-definition dry run: %+v", request)
			}
			subject := definition.Subject{TenantID: scan.GenerateID(), ActorID: scan.GenerateID()}
			backend := &exampleDefinitionBackend{tenant: subject.TenantID}
			store := &exampleDefinitionStore{}
			service := definition.Service{Backend: backend, Store: store, Images: exampleImageResolver{}}
			ctx := definition.WithSubject(context.Background(), subject)
			result, err := service.Apply(ctx, kind, "finance-prod", "billing", request)
			if err != nil {
				t.Fatalf("standard creation example is unusable: %v", err)
			}
			if result.State != "validated" || backend.calls != 1 || store.registered == nil || store.registered.ActorID != subject.ActorID || store.registered.State != "checked" {
				t.Fatalf("example did not retain its checked intent: result=%+v calls=%d operation=%+v", result, backend.calls, store.registered)
			}
			var spec map[string]json.RawMessage
			if err := json.Unmarshal([]byte(request.SpecJSON), &spec); err != nil {
				t.Fatal(err)
			}
			delete(spec, "executionPolicyVersion")
			legacy, err := json.Marshal(spec)
			if err != nil {
				t.Fatal(err)
			}
			request.SpecJSON = string(legacy)
			if _, err := service.Apply(ctx, kind, "finance-prod", "billing", request); !validation.Is(err) || backend.calls != 1 {
				t.Fatalf("standard service bypassed explicit creation policy: err=%v calls=%d", err, backend.calls)
			}
		})
	}
}
