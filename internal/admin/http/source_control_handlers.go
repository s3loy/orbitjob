package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	app "orbitjob/internal/admin/app/sourcecontrol"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/sourcecontrol"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/platform/strictjson"
)

type sourceControlService interface {
	Resolve(context.Context, string, string, int64) (app.Target, error)
	Get(context.Context, app.Target) (sourcecontrol.State, error)
	Update(context.Context, app.Target, app.Change, string) (sourcecontrol.State, error)
}

func (h *Handler) SetSourceControlService(service sourceControlService) { h.sourceControls = service }

type sourceControlContextKey struct{}
type sourceControlPath struct {
	ID       int64  `uri:"id" binding:"required,min=1"`
	TenantID string `form:"tenant_id"`
}
type SourceControlUpdateRequest struct {
	Version         *int64 `json:"version" binding:"required,min=0"`
	SchedulePaused  *bool  `json:"schedule_paused" binding:"required"`
	ExecutionPaused *bool  `json:"execution_paused" binding:"required"`
}
type SourceControlResponse struct {
	SourceMode      string     `json:"source_mode"`
	SourceUID       string     `json:"source_uid"`
	Version         int64      `json:"version"`
	SchedulePaused  bool       `json:"schedule_paused"`
	ExecutionPaused bool       `json:"execution_paused"`
	Tombstoned      bool       `json:"tombstoned"`
	CanManage       bool       `json:"can_manage"`
	Actor           string     `json:"actor,omitempty"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

func sourceControlARN(target app.Target) policy.ARN {
	group := target.ResourceGroupID
	if group == "" {
		group = "*"
	}
	return policy.ARN{Tenant: target.TenantID, Group: group, Type: strings.TrimSuffix(target.Kind, "s"), ID: strconv.FormatInt(target.ID, 10)}
}

func (h *Handler) sourceControlResource(kind string) middleware.ResourceResolver {
	return func(c *gin.Context) (policy.ARN, error) {
		principal, ok := middleware.PrincipalFrom(c)
		if !ok {
			return policy.ARN{}, errNoPrincipal
		}
		tenant, err := middleware.ResolveEffectiveTenant(c, c.Query("tenant_id"))
		if err != nil {
			return policy.ARN{}, err
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id < 1 {
			return policy.ARN{}, validation.New("id", "must be positive")
		}
		if h == nil || h.sourceControls == nil {
			return policy.ARN{}, middleware.ErrAuthorizationLookup
		}
		target, err := h.sourceControls.Resolve(c.Request.Context(), tenant, kind, id)
		if err != nil {
			return policy.ARN{}, err
		}
		if target.TenantID != tenant || target.Kind != kind || target.ID != id || target.SourceUID == "" {
			return policy.ARN{}, middleware.ErrAuthorizationLookup
		}
		if principal.ResourceGroupID != "" && principal.ResourceGroupID != target.ResourceGroupID {
			return policy.ARN{}, &resource.NotFoundError{Resource: kind, ID: id}
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), sourceControlContextKey{}, target))
		return sourceControlARN(target), nil
	}
}

func (h *Handler) sourceControlResponse(c *gin.Context, target app.Target, state sourcecontrol.State) SourceControlResponse {
	docs, _ := middleware.Documents(c)
	canManage := policy.Evaluate(docs, policy.Request{Action: strings.TrimSuffix(target.Kind, "s") + ":ManageExecution", Resource: sourceControlARN(target)}) == policy.DecisionAllow
	out := SourceControlResponse{SourceMode: state.Mode, SourceUID: state.SourceUID, Version: state.Version, SchedulePaused: state.SchedulePaused, ExecutionPaused: state.ExecutionPaused, Tombstoned: state.Tombstoned, CanManage: canManage, Actor: state.Actor}
	if !state.UpdatedAt.IsZero() {
		out.UpdatedAt = &state.UpdatedAt
	}
	return out
}

func (h *Handler) sourceControlHandler(update bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		target, ok := c.Request.Context().Value(sourceControlContextKey{}).(app.Target)
		if !ok {
			writeAPIError(c, middleware.ErrAuthorizationLookup)
			return
		}
		if !update {
			state, err := h.sourceControls.Get(c.Request.Context(), target)
			if err != nil {
				writeAPIError(c, err)
				return
			}
			c.JSON(http.StatusOK, h.sourceControlResponse(c, target, state))
			return
		}
		var req SourceControlUpdateRequest
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, strictjson.MaxBytes)
		body, err := io.ReadAll(c.Request.Body)
		if err != nil || strictjson.Validate(body) != nil {
			writeAPIError(c, validation.New("body", "use one bounded JSON object without duplicate fields"))
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeAPIError(c, validation.New("body", "invalid execution controls"))
			return
		}
		if err := binding.Validator.ValidateStruct(req); err != nil {
			writeAPIError(c, validation.New("body", "version and both pause fields are required"))
			return
		}
		state, err := h.sourceControls.Update(c.Request.Context(), target, app.Change{Version: *req.Version, SchedulePaused: *req.SchedulePaused, ExecutionPaused: *req.ExecutionPaused}, actorID(c))
		if err != nil {
			writeAPIError(c, err)
			return
		}
		c.JSON(http.StatusOK, h.sourceControlResponse(c, target, state))
	}
}
