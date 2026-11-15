package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"bytes"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"orbitjob/internal/admin/definition"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/admin/logs"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/strictjson"
)

type definitionBackend interface {
	Tenant(string) (string, bool)
	Get(context.Context, string, string, string) (definition.Result, error)
	Apply(context.Context, string, string, string, definition.ApplyRequest) (definition.Result, error)
}
type logBackend interface {
	Read(context.Context, logs.Locator, logs.Request) (logs.Result, error)
}

func (h *Handler) SetDefinitionBackend(d definitionBackend)  { h.definitions = d }
func (h *Handler) SetLogBackend(r logs.Reader, l logBackend) { h.logLocators = r; h.workloadLogs = l }
func definitionARN(kind string) func(*gin.Context) (policy.ARN, error) {
	return func(c *gin.Context) (policy.ARN, error) {
		tenant, err := middleware.ResolveEffectiveTenant(c, c.Query("tenant_id"))
		if err != nil {
			return policy.ARN{}, err
		}
		if groupFrom(c) != "" {
			return policy.ARN{}, middleware.ErrResourceUnavailable
		}
		typ := "job"
		if kind == "workflows" {
			typ = "workflow"
		}
		return policy.ARN{Tenant: tenant, Group: "*", Type: typ, ID: c.Param("namespace") + "/" + c.Param("name")}, nil
	}
}
func (h *Handler) definitionTarget(c *gin.Context) (string, bool) {
	tenant, err := middleware.ResolveEffectiveTenant(c, c.Query("tenant_id"))
	ok := err == nil
	if !ok {
		writeForbidden(c, "invalid tenant target")
	}
	if !ok {
		return "", false
	}
	mapped, ok := h.definitions.Tenant(c.Param("namespace"))
	if !ok || mapped != tenant || groupFrom(c) != "" {
		writeForbidden(c, "namespace is not mapped to this tenant or credential scope")
		return "", false
	}
	return tenant, true
}
func (h *Handler) getDefinition(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := h.definitionTarget(c); !ok {
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		out, err := h.definitions.Get(ctx, kind, c.Param("namespace"), c.Param("name"))
		if err != nil {
			writeAPIError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, out)
	}
}
func (h *Handler) applyDefinition(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenant, ok := h.definitionTarget(c)
		if !ok {
			return
		}
		var in definition.ApplyRequest
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
		body, readErr := io.ReadAll(c.Request.Body)
		if readErr != nil || strictjson.Validate(body) != nil {
			writeAPIError(c, validation.New("body", "use one bounded JSON object without duplicate fields"))
			return
		}
		d := json.NewDecoder(bytes.NewReader(body))
		d.DisallowUnknownFields()
		if err := d.Decode(&in); err != nil {
			writeAPIError(c, validation.New("body", "invalid typed apply request"))
			return
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			writeAPIError(c, validation.New("body", "exactly one request required"))
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		principal, ok := middleware.PrincipalFrom(c)
		if !ok {
			writeForbidden(c, "authenticated identity required")
			return
		}
		ctx = definition.WithSubject(ctx, definition.Subject{TenantID: tenant, ActorID: principal.ActorID()})
		var out definition.Result
		var err error
		if c.FullPath() == "/api/v1/definitions/"+kind+"/:namespace/:name/compat" {
			compat, supported := h.definitions.(interface {
				ApplyCompatible(context.Context, string, string, string, definition.ApplyRequest) (definition.Result, error)
			})
			if !supported {
				writeAPIError(c, &resource.UnavailableError{Resource: "legacy definition adapter"})
				return
			}
			c.Header("X-OrbitJob-Compatibility", "checked-save-v1")
			out, err = compat.ApplyCompatible(ctx, kind, c.Param("namespace"), c.Param("name"), in)
		} else {
			out, err = h.definitions.Apply(ctx, kind, c.Param("namespace"), c.Param("name"), in)
		}
		if err != nil {
			writeAPIError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, out)
	}
}

func (h *Handler) getDefinitionOperation(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenant, ok := h.definitionTarget(c)
		if !ok {
			return
		}
		if _, err := uuid.Parse(c.Param("request_id")); err != nil {
			writeAPIError(c, validation.New("request_id", "UUID required"))
			return
		}
		principal, ok := middleware.PrincipalFrom(c)
		if !ok {
			writeForbidden(c, "authenticated identity required")
			return
		}
		backend, ok := h.definitions.(interface {
			Operation(context.Context, string, string, string, string) (definition.Operation, error)
		})
		if !ok {
			writeAPIError(c, &resource.UnavailableError{Resource: "definition operations"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		ctx = definition.WithSubject(ctx, definition.Subject{TenantID: tenant, ActorID: principal.ActorID()})
		out, err := backend.Operation(ctx, kind, c.Param("namespace"), c.Param("name"), c.Param("request_id"))
		if err != nil {
			writeAPIError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, out)
	}
}
func (h *Handler) readLogs(function bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenant, err := middleware.ResolveEffectiveTenant(c, c.Query("tenant_id"))
		ok := err == nil
		if !ok {
			writeForbidden(c, "invalid tenant target")
		}
		if !ok {
			return
		}
		attempt, err := strconv.Atoi(c.Param("attempt"))
		if err != nil || attempt < 1 {
			writeAPIError(c, validation.New("attempt", "positive attempt number required"))
			return
		}
		for name := range c.Request.URL.Query() {
			if name != "tenant_id" && name != "tail_lines" && name != "limit_bytes" {
				writeAPIError(c, validation.New("query", "unsupported log parameter"))
				return
			}
		}
		var in logs.Request
		if err = c.ShouldBindQuery(&in); err != nil {
			writeAPIError(c, validation.New("query", "invalid log snapshot limits"))
			return
		}
		var loc logs.Locator
		if function {
			id, e := strconv.ParseInt(c.Param("id"), 10, 64)
			if e != nil || id < 1 {
				writeAPIError(c, validation.New("id", "positive function ID required"))
				return
			}
			if _, e = uuid.Parse(c.Param("run_id")); e != nil {
				writeAPIError(c, validation.New("run_id", "function history UUID required"))
				return
			}
			if cached, ok := c.Get("authorized_log_locator"); ok {
				loc = cached.(logs.Locator)
			} else {
				loc, err = h.logLocators.ResolveFunction(c.Request.Context(), tenant, id, c.Param("run_id"), attempt)
			}
		} else {
			if err = resource.RequireUnscoped(groupFrom(c), "instances"); err != nil {
				writeAPIError(c, err)
				return
			}
			id, e := strconv.ParseInt(c.Param("run_id"), 10, 64)
			if e != nil || id < 1 {
				writeAPIError(c, validation.New("run_id", "positive ledger run ID required"))
				return
			}
			loc, err = h.logLocators.Resolve(c.Request.Context(), tenant, id, attempt)
		}
		if err != nil {
			writeAPIError(c, err)
			return
		}
		if group := groupFrom(c); group != "" && loc.GroupID != group {
			writeAPIError(c, &resource.NotFoundError{Resource: "function execution"})
			return
		}
		// Re-evaluate the exact Function's actual group after the authoritative read;
		// a wildcard credential must not turn an exact-group Deny into an Allow.
		if function {
			docs, ok := middleware.Documents(c)
			if !ok {
				writeForbidden(c, "authorization context missing")
				return
			}
			group := loc.GroupID
			if group == "" {
				group = "*"
			}
			if policy.Evaluate(docs, policy.Request{Action: "function:ReadLogs", Resource: policy.ARN{Tenant: tenant, Group: group, Type: "function", ID: c.Param("id")}}) != policy.DecisionAllow {
				writeForbidden(c, "function log access denied")
				return
			}
		}
		out, err := h.workloadLogs.Read(c.Request.Context(), loc, in)
		if err != nil {
			writeAPIError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, out)
	}
}

// Resolve the exact Function parent before the shared policy guard. This also
// supports policies restricted to a concrete group on an ungrouped credential.
func (h *Handler) functionLogResource(c *gin.Context) (policy.ARN, error) {
	tenant, err := middleware.ResolveEffectiveTenant(c, c.Query("tenant_id"))
	if err != nil {
		return policy.ARN{}, err
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return policy.ARN{}, middleware.ErrResourceUnavailable
	}
	attempt, err := strconv.Atoi(c.Param("attempt"))
	if err != nil || attempt < 1 {
		return policy.ARN{}, middleware.ErrResourceUnavailable
	}
	if _, err = uuid.Parse(c.Param("run_id")); err != nil {
		return policy.ARN{}, middleware.ErrResourceUnavailable
	}
	loc, err := h.logLocators.ResolveFunction(c.Request.Context(), tenant, id, c.Param("run_id"), attempt)
	if err != nil {
		return policy.ARN{}, middleware.ErrResourceUnavailable
	}
	if group := groupFrom(c); group != "" && loc.GroupID != group {
		return policy.ARN{}, middleware.ErrResourceUnavailable
	}
	group := loc.GroupID
	if group == "" {
		group = "*"
	}
	c.Set("authorized_log_locator", loc)
	return policy.ARN{Tenant: tenant, Group: group, Type: "function", ID: c.Param("id")}, nil
}

// The deadline includes policy parent lookup and ledger reads, not just the
// final Kubernetes stream. Require calls c.Next synchronously inside this scope.
func logDeadline(guard gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		guard(c)
	}
}

func instanceLogARN(c *gin.Context) (policy.ARN, error) {
	tenant, err := middleware.ResolveEffectiveTenant(c, c.Query("tenant_id"))
	if err != nil {
		return policy.ARN{}, err
	}
	if groupFrom(c) != "" {
		return policy.ARN{}, middleware.ErrResourceUnavailable
	}
	return policy.ARN{Tenant: tenant, Group: "*", Type: "instance", ID: c.Param("run_id")}, nil
}
