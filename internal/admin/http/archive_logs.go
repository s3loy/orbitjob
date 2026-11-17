package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"orbitjob/internal/admin/http/apperror"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/domain/validation"
	"orbitjob/internal/logarchive"
)

type archiveReader interface {
	Read(context.Context, logarchive.Identity, logarchive.Query) (logarchive.Page, error)
}
type archiveDirectory interface {
	Resolve(context.Context, string, int64, int64, string) (logarchive.Identity, error)
	RequestCleanup(context.Context, logarchive.Identity, string) error
}

func (h *Handler) SetArchiveBackend(directory archiveDirectory, reader archiveReader) {
	h.archiveDirectory = directory
	h.archiveReader = reader
}

func (h *Handler) archiveResource(function bool) middleware.ResourceResolver {
	return func(c *gin.Context) (policy.ARN, error) {
		tenant, e := middleware.ResolveEffectiveTenant(c, c.Query("tenant_id"))
		if e != nil {
			return policy.ARN{}, e
		}
		var run, fn int64
		var functionRun string
		var arn policy.ARN
		if function {
			fn, e = strconv.ParseInt(c.Param("id"), 10, 64)
			if e != nil || fn < 1 {
				return arn, validation.New("id", "positive Function id required")
			}
			functionRun = c.Param("run_id")
			if _, e = uuid.Parse(functionRun); e != nil {
				return arn, validation.New("run_id", "Function history UUID required")
			}
			arn, e = h.functionResource(c)
			if e != nil {
				return arn, e
			}
		} else {
			if e = resource.RequireUnscoped(groupFrom(c), "instances"); e != nil {
				return arn, middleware.ErrResourceUnavailable
			}
			run, e = strconv.ParseInt(c.Param("run_id"), 10, 64)
			if e != nil || run < 1 {
				return arn, validation.New("run_id", "positive execution id required")
			}
			arn = policy.ARN{Tenant: tenant, Group: "*", Type: "instance", ID: c.Param("run_id")}
		}
		id, e := h.archiveDirectory.Resolve(c.Request.Context(), tenant, run, fn, functionRun)
		if errors.Is(e, logarchive.ErrMissing) {
			return arn, &resource.NotFoundError{Resource: "execution log archive"}
		}
		if e != nil {
			return arn, e
		}
		if function && (id.FunctionID != fn || id.FunctionRunID != functionRun) {
			return arn, &resource.NotFoundError{Resource: "function execution archive"}
		}
		c.Set("authorized_archive_identity", id)
		return arn, nil
	}
}

func (h *Handler) readArchive(download bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		for key := range c.Request.URL.Query() {
			if key != "tenant_id" && key != "after" && key != "limit" && key != "attempt" && key != "search" {
				writeAPIError(c, validation.New("query", "unknown archive query parameter"))
				return
			}
		}
		var q logarchive.Query
		if e := c.ShouldBindQuery(&q); e != nil {
			apperror.Write(c, http.StatusBadRequest, toBindAPIError(e))
			return
		}
		value, ok := c.Get("authorized_archive_identity")
		if !ok {
			writeForbidden(c, "archive authorization context missing")
			return
		}
		id := value.(logarchive.Identity)
		page, e := h.archiveReader.Read(c.Request.Context(), id, q)
		if e != nil {
			apperror.Write(c, http.StatusServiceUnavailable, apperror.APIError{Code: apperror.CodeServiceUnavailable, Message: "execution archive temporarily unavailable"})
			return
		}
		c.Header("Cache-Control", "no-store")
		if !download {
			c.JSON(http.StatusOK, page)
			return
		}
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename="execution-logs.txt"`)
		_, _ = fmt.Fprintf(c.Writer, "# OrbitJob execution %d\n# Collection: %s — %s\n", id.RunID, page.State, page.Reason)
		state, reason := page.State, page.Reason
		lostState := ""
		seenGaps := make(map[logarchive.Gap]bool)
		for pages := 0; pages < 100000; pages++ {
			if lostState == "" && (page.State == "expired" || page.State == "deleted" || page.State == "unavailable") {
				lostState = page.State
			}
			if page.State != state || page.Reason != reason {
				if _, e = fmt.Fprintf(c.Writer, "\n# Collection: %s — %s\n", page.State, page.Reason); e != nil {
					return
				}
				state, reason = page.State, page.Reason
			}
			for _, gap := range page.Gaps {
				if seenGaps[gap] {
					continue
				}
				if len(seenGaps) >= 1024 {
					_, _ = fmt.Fprint(c.Writer, "\n# EXPORT INCOMPLETE: omission metadata limit\n")
					return
				}
				seenGaps[gap] = true
				extent := "(range unknown)"
				if gap.From > 0 {
					extent = fmt.Sprintf("%d-%d", gap.From, gap.Through)
				}
				if _, e = fmt.Fprintf(c.Writer, "# OUTPUT GAP %s: %s\n", extent, gap.Reason); e != nil {
					return
				}
			}
			for _, record := range page.Records {
				if record.Kind == "event" {
					_, e = fmt.Fprintf(c.Writer, "\n# %s [attempt %d] %s\n", record.Time.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), record.Attempt, record.Text)
				} else {
					_, e = fmt.Fprint(c.Writer, record.Text)
				}
				if e != nil {
					return
				}
			}
			if !page.HasMore {
				if lostState != "" {
					_, _ = fmt.Fprintf(c.Writer, "\n# EXPORT INCOMPLETE: logs %s; only output fetched above is included.\n", lostState)
				} else {
					_, _ = fmt.Fprint(c.Writer, "\n# END OF RETAINED LOG EXPORT\n")
				}
				return
			}
			if c.Request.Context().Err() != nil {
				return
			}
			q.After = page.Next
			page, e = h.archiveReader.Read(c.Request.Context(), id, q)
			if e != nil {
				_, _ = fmt.Fprint(c.Writer, "\n# EXPORT INCOMPLETE: archive unavailable\n")
				return
			}
			if page.HasMore && page.Next <= q.After {
				_, _ = fmt.Fprint(c.Writer, "\n# EXPORT INCOMPLETE: archive cursor did not advance\n")
				return
			}
		}
		_, _ = fmt.Fprint(c.Writer, "\n# EXPORT INCOMPLETE: bounded export limit\n")
	}
}

func (h *Handler) requestArchiveCleanup(c *gin.Context) {
	value, ok := c.Get("authorized_archive_identity")
	if !ok {
		writeForbidden(c, "archive authorization context missing")
		return
	}
	if e := h.archiveDirectory.RequestCleanup(c.Request.Context(), value.(logarchive.Identity), actorID(c)); e != nil {
		writeAPIError(c, e)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"state": "requested"})
}
