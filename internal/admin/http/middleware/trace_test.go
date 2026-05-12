package middleware

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTraceMiddleware_GeneratesNewID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(stdhttp.MethodGet, "/", nil)

	TraceMiddleware()(c)

	traceID := c.Writer.Header().Get("X-Trace-ID")
	if traceID == "" {
		t.Fatal("expected X-Trace-ID header to be set")
	}
	if len(traceID) < 16 {
		t.Fatalf("expected trace id length >= 16, got %d", len(traceID))
	}
}

func TestTraceMiddleware_ForwardsExistingID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	req.Header.Set("X-Trace-ID", "existing-trace-123")
	c.Request = req

	TraceMiddleware()(c)

	traceID := c.Writer.Header().Get("X-Trace-ID")
	if traceID != "existing-trace-123" {
		t.Fatalf("expected X-Trace-ID=existing-trace-123, got %q", traceID)
	}
}

func TestTraceMiddleware_SetsContextValue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(stdhttp.MethodGet, "/", nil)

	var captured string
	router := gin.New()
	router.Use(TraceMiddleware())
	router.GET("/", func(c *gin.Context) {
		val, ok := c.Get("trace_id")
		if ok {
			captured, _ = val.(string)
		}
		c.JSON(stdhttp.StatusOK, gin.H{"ok": true})
	})

	router.ServeHTTP(w, c.Request)

	if captured == "" {
		t.Fatal("expected trace_id to be set in gin context")
	}
}
