package definition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
)

type Subject struct{ TenantID, ActorID string }
type subjectKey struct{}

// WithSubject carries only identity resolved by the authenticated HTTP boundary.
func WithSubject(ctx context.Context, subject Subject) context.Context {
	return context.WithValue(ctx, subjectKey{}, subject)
}

func subjectFrom(ctx context.Context) (Subject, error) {
	s, ok := ctx.Value(subjectKey{}).(Subject)
	if !ok || s.TenantID == "" || s.ActorID == "" {
		return Subject{}, &resource.ScopeError{Resource: "definition operation", Scope: "missing authenticated identity"}
	}
	return s, nil
}

type Operation struct {
	AdmissionProof string       `json:"-"`
	RequestID      string       `json:"request_id"`
	ActorID        string       `json:"actor_id"`
	Kind           string       `json:"kind"`
	Namespace      string       `json:"namespace"`
	Name           string       `json:"name"`
	PayloadHash    string       `json:"-"`
	AdmittedUID    string       `json:"admitted_uid,omitempty"`
	State          string       `json:"state"`
	Request        ApplyRequest `json:"-"`
	Result         *Result      `json:"result,omitempty"`
	CheckedUntil   time.Time    `json:"checked_until"`
	ExpiresAt      time.Time    `json:"expires_at"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

type OperationStore interface {
	Register(context.Context, Subject, Operation) error
	Lookup(context.Context, Subject, string) (Operation, error)
	Claim(context.Context, Subject, string, string, time.Time) (Operation, bool, error)
	Finish(context.Context, Subject, string, string, *Result, time.Time) error
}

type Backend interface {
	Tenant(string) (string, bool)
	Get(context.Context, string, string, string) (Result, error)
	Apply(context.Context, string, string, string, ApplyRequest) (Result, error)
}

// Service enforces a retained check and claims an intent before external writes.
// A claimed intent is never dispatched a second time after an uncertain result.
type Service struct {
	Backend              Backend
	Store                OperationStore
	Now                  func() time.Time
	Images               ImageResolver
	AllowLegacyAuthoring bool
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s Service) Tenant(namespace string) (string, bool) { return s.Backend.Tenant(namespace) }
func (s Service) Get(ctx context.Context, kind, namespace, name string) (Result, error) {
	return s.Backend.Get(ctx, kind, namespace, name)
}

func operationHash(kind, namespace, name string, in ApplyRequest) string {
	b, _ := json.Marshal([]string{kind, namespace, name, in.ExpectedUID, in.ExpectedResourceVersion, in.SpecJSON})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func unknownOperation() error {
	return &resource.UnavailableError{Resource: "definition operation", Message: "save outcome is unknown; check the original operation without submitting another write"}
}

func (s Service) Apply(ctx context.Context, kind, namespace, name string, in ApplyRequest) (Result, error) {
	subject, err := subjectFrom(ctx)
	if err != nil {
		return Result{}, err
	}
	if s.Store == nil {
		return Result{}, &resource.UnavailableError{Resource: "definition operations"}
	}
	if tenant, ok := s.Tenant(namespace); !ok || tenant != subject.TenantID {
		return Result{}, &resource.ScopeError{Resource: "definition", Scope: "namespace tenant"}
	}
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return Result{}, validation.New("request_id", "UUID required")
	}
	canonical, digest, err := CanonicalSpec(kind, in.SpecJSON)
	if err != nil {
		return Result{}, err
	}
	in.SpecJSON = canonical
	if in.DryRun {
		version, err := policyVersion(in.SpecJSON)
		if err != nil {
			return Result{}, err
		}
		legacy, _ := ctx.Value(legacyAuthoringKey{}).(bool)
		if in.ExpectedUID == "" && version != 1 && !legacy {
			return Result{}, validation.New("executionPolicyVersion", "new definitions require execution policy version 1; use the bounded editor or the administrator-enabled compatibility adapter")
		}
		in.SpecJSON, err = s.prepareImage(ctx, kind, in.SpecJSON)
		if err != nil {
			return Result{}, err
		}
		out, err := s.Backend.Apply(ctx, kind, namespace, name, in)
		if err != nil {
			return Result{}, err
		}
		if out.RequestID != in.RequestID || out.Kind != kind || out.Namespace != namespace || out.Name != name || out.State != "validated" {
			return Result{}, fmt.Errorf("definition check returned an inconsistent identity")
		}
		in.SpecJSON, in.ManifestDigest = out.SpecJSON, out.ManifestDigest
		now := s.now()
		op := Operation{RequestID: in.RequestID, ActorID: subject.ActorID, Kind: kind, Namespace: namespace, Name: name, PayloadHash: operationHash(kind, namespace, name, in), State: "checked", Request: in, Result: &out, CheckedUntil: now.Add(10 * time.Minute), ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedAt: now, UpdatedAt: now}
		if err := s.Store.Register(ctx, subject, op); err != nil {
			return Result{}, err
		}
		return out, nil
	}
	if in.ManifestDigest != digest {
		return Result{}, validation.New("manifest_digest", "check and review this exact configuration before saving")
	}
	op, claimed, err := s.Store.Claim(ctx, subject, in.RequestID, operationHash(kind, namespace, name, in), s.now())
	if err != nil {
		return Result{}, err
	}
	if op.Kind != kind || op.Namespace != namespace || op.Name != name {
		return Result{}, &resource.NotFoundError{Resource: "definition operation"}
	}
	if !claimed {
		if op.State == "accepted" && op.Result != nil {
			out := *op.Result
			out.State = "reconciled"
			return out, nil
		}
		return Result{}, unknownOperation()
	}
	in.AdmissionProof = op.AdmissionProof
	out, applyErr := s.Backend.Apply(ctx, kind, namespace, name, in)
	state := "accepted"
	if applyErr != nil {
		state = "unknown"
		var conflict *resource.ConflictError
		var scope *resource.ScopeError
		if validation.Is(applyErr) || errors.As(applyErr, &conflict) || errors.As(applyErr, &scope) {
			state = "rejected"
		}
	} else if out.RequestID != in.RequestID || out.Kind != kind || out.Namespace != namespace || out.Name != name || out.ManifestDigest != digest || out.UID == "" || (in.ExpectedUID != "" && out.UID != in.ExpectedUID) || (out.State != "accepted" && out.State != "reconciled") {
		state, applyErr = "unknown", fmt.Errorf("definition save returned an inconsistent identity")
	}
	var result *Result
	if state == "accepted" {
		result = &out
	}
	// Preserve the known outcome even when the browser has stopped waiting.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := s.Store.Finish(finishCtx, subject, in.RequestID, state, result, s.now()); err != nil {
		return Result{}, unknownOperation()
	}
	if state == "unknown" {
		return Result{}, unknownOperation()
	}
	return out, applyErr
}

func (s Service) Operation(ctx context.Context, kind, namespace, name, requestID string) (Operation, error) {
	if s.Store == nil {
		return Operation{}, &resource.UnavailableError{Resource: "definition operations"}
	}
	subject, err := subjectFrom(ctx)
	if err != nil {
		return Operation{}, err
	}
	op, err := s.Store.Lookup(ctx, subject, requestID)
	if err != nil {
		return Operation{}, err
	}
	if op.Kind != kind || op.Namespace != namespace || op.Name != name {
		return Operation{}, &resource.NotFoundError{Resource: "definition operation"}
	}
	if op.State != "dispatching" && op.State != "unknown" {
		return op, nil
	}
	live, err := s.Backend.Get(ctx, kind, namespace, name)
	if err != nil {
		return op, nil
	}
	// An update knows its original UID. A lost create response needs a UID from
	// durable admission evidence; copied annotations alone cannot prove it.
	expectedUID := op.Request.ExpectedUID
	if expectedUID == "" {
		expectedUID = op.AdmittedUID
	}
	if expectedUID == "" {
		return op, nil
	}
	if live.UID != expectedUID || live.LastRequestID != requestID || live.LastManifestDigest != op.Request.ManifestDigest || live.ManifestDigest != op.Request.ManifestDigest {
		return op, nil
	}
	live.RequestID, live.State = requestID, "reconciled"
	if err := s.Store.Finish(ctx, subject, requestID, "accepted", &live, s.now()); err != nil {
		return op, err
	}
	op.State, op.Result, op.UpdatedAt = "accepted", &live, s.now()
	return op, nil
}

// ApplyCompatible supports a named legacy entry point without bypassing checks.
// It refuses admission defaults that would change the client's execution intent.
func (s Service) ApplyCompatible(ctx context.Context, kind, namespace, name string, in ApplyRequest) (Result, error) {
	if !s.AllowLegacyAuthoring {
		return Result{}, &resource.ScopeError{Resource: "legacy authoring", Scope: "compatibility adapter is not enabled by the installation administrator"}
	}
	ctx = context.WithValue(ctx, legacyAuthoringKey{}, true)
	if s.Store == nil {
		return Result{}, &resource.UnavailableError{Resource: "definition operations"}
	}
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return Result{}, validation.New("request_id", "UUID required")
	}
	if in.DryRun {
		return s.Apply(ctx, kind, namespace, name, in)
	}
	subject, err := subjectFrom(ctx)
	if err != nil {
		return Result{}, err
	}
	canonical, digest, err := CanonicalSpec(kind, in.SpecJSON)
	if err != nil {
		return Result{}, err
	}
	in.SpecJSON = canonical
	if op, lookupErr := s.Store.Lookup(ctx, subject, in.RequestID); lookupErr == nil {
		in.ManifestDigest = digest
		if op.PayloadHash != operationHash(kind, namespace, name, in) {
			return Result{}, &resource.ConflictError{Resource: "definition operation", Message: "legacy request identity belongs to a different intent"}
		}
		return s.Apply(ctx, kind, namespace, name, in)
	} else {
		var missing *resource.NotFoundError
		if !errors.As(lookupErr, &missing) {
			return Result{}, lookupErr
		}
	}
	check := in
	check.DryRun = true
	out, err := s.Apply(ctx, kind, namespace, name, check)
	if err != nil {
		return Result{}, err
	}
	if out.ManifestDigest != digest {
		return Result{}, &resource.ConflictError{Resource: "definition", Field: "spec_json", Message: "cluster defaults changed the execution configuration; upgrade the client and review the checked configuration before saving"}
	}
	in.ManifestDigest = out.ManifestDigest
	return s.Apply(ctx, kind, namespace, name, in)
}
