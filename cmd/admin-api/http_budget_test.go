package main

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/core/app/functioninvoke"
)

func TestHTTPWriteBudgetIncludesMaximumFunctionWait(t *testing.T) {
	srv := newHTTPServer(":0", nil)
	wait := time.Duration(functioninvoke.MaxInvokeWaitSeconds) * time.Second
	if srv.WriteTimeout <= wait {
		t.Fatalf("write budget %s cannot deliver a %s synchronous wait", srv.WriteTimeout, wait)
	}
}

func TestRouterSetsBoundedRequestDeadline(t *testing.T) {
	router := newRouter(nil, nil, nil)
	var observed time.Time
	router.GET("/test-deadline", func(c *gin.Context) {
		var ok bool
		observed, ok = c.Request.Context().Deadline()
		if !ok {
			t.Error("handler received no deadline")
		}
		c.Status(204)
	})
	started := time.Now()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/test-deadline", nil))
	if observed.Before(started.Add(adminRequestTimeout-time.Second)) || observed.After(time.Now().Add(adminRequestTimeout)) {
		t.Fatalf("handler deadline %s does not match request budget %s", observed, adminRequestTimeout)
	}
	// A client's earlier cancellation budget still wins.
	earlier := time.Now().Add(time.Second)
	ctx, cancel := context.WithDeadline(t.Context(), earlier)
	defer cancel()
	request := httptest.NewRequest("GET", "/test-deadline", nil).WithContext(ctx)
	router.ServeHTTP(httptest.NewRecorder(), request)
	if !observed.Equal(earlier) {
		t.Fatalf("replaced client's earlier deadline: %s", observed)
	}
}
