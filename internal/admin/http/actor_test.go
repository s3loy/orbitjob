package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequiredActorID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		header     string
		wantActor  string
		wantErrMsg string
	}{
		{
			name:      "success",
			header:    " control-plane-user ",
			wantActor: "control-plane-user",
		},
		{
			name:       "missing actor",
			wantErrMsg: "actor_id: is required",
		},
		{
			name:       "actor too long",
			header:     strings.Repeat("x", 129),
			wantErrMsg: "actor_id: must be <= 128 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set(actorIDHeader, tt.header)
			}

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = req

			got, err := requiredActorID(c)
			if tt.wantErrMsg == "" {
				if err != nil {
					t.Fatalf("requiredActorID() error = %v", err)
				}
				if got != tt.wantActor {
					t.Fatalf("expected actor_id=%q, got %q", tt.wantActor, got)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if err.Error() != tt.wantErrMsg {
				t.Fatalf("expected error message=%q, got %q", tt.wantErrMsg, err.Error())
			}
		})
	}
}
