package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestWorkflowSchedulePreviewValidatesAndPreservesUTCInstants(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body, group string
		status            int
	}{
		{"zone", `{"schedule":"0 9 * * *","time_zone":"Asia/Shanghai","count":3}`, "", 200},
		{"default", `{"schedule":"0 * * * *"}`, "", 200},
		{"cron", `{"schedule":"@daily"}`, "", 400},
		{"zone_invalid", `{"schedule":"* * * * *","time_zone":"Local"}`, "", 400},
		{"count", `{"schedule":"* * * * *","count":21}`, "", 400},
		{"empty", `{"schedule":""}`, "", 400},
		{"scope", `{"schedule":"* * * * *"}`, "group-a", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(nil, nil, nil)
			router := gin.New()
			router.Use(testPrincipalMiddleware("tenant-a", "key-1", tc.group))
			router.POST("/preview", h.PreviewWorkflowSchedule)
			request := httptest.NewRequest(http.MethodPost, "/preview", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.status == 200 {
				var out WorkflowSchedulePreviewResult
				if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if len(out.Upcoming) == 0 || out.TimeZone == "" {
					t.Fatalf("missing preview=%+v", out)
				}
				for i, at := range out.Upcoming {
					if at.Location() != time.UTC || !at.After(time.Now().Add(-time.Minute)) || (i > 0 && !at.After(out.Upcoming[i-1])) {
						t.Fatalf("invalid UTC preview=%v", out.Upcoming)
					}
				}
			}
		})
	}
}
