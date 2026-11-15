package http

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"orbitjob/internal/admin/definition"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/admin/logs"
	"orbitjob/internal/core/domain/policy"
)

type fakeDefinitions struct{ called bool }

func (d *fakeDefinitions) Tenant(ns string) (string, bool) { return usecaseTestTenant, ns == "own" }
func (d *fakeDefinitions) Get(context.Context, string, string, string) (definition.Result, error) {
	d.called = true
	return definition.Result{}, nil
}
func (d *fakeDefinitions) Apply(context.Context, string, string, string, definition.ApplyRequest) (definition.Result, error) {
	d.called = true
	return definition.Result{State: "validated"}, nil
}

type fakeLogReader struct {
	group  string
	called bool
}

func (r *fakeLogReader) Resolve(context.Context, string, int64, int) (logs.Locator, error) {
	r.called = true
	return logs.Locator{TenantID: usecaseTestTenant, Attempt: 1}, nil
}
func (r *fakeLogReader) ResolveFunction(context.Context, string, int64, string, int) (logs.Locator, error) {
	r.called = true
	return logs.Locator{TenantID: usecaseTestTenant, Attempt: 1, GroupID: r.group}, nil
}

type fakeLogBackend struct{ called bool }

func (b *fakeLogBackend) Read(context.Context, logs.Locator, logs.Request) (logs.Result, error) {
	b.called = true
	return logs.Result{State: "ready", Text: "bounded"}, nil
}
func TestDefinitionRoutesDenyWithoutApplyAndRefuseForeignTargets(t *testing.T) {
	for _, test := range []struct {
		name, action, group, path, body string
		status                          int
	}{
		{"read does not imply apply", "job:Get", "", "/api/v1/definitions/jobs/own/task", `{"spec_json":"{}"}`, 403},
		{"foreign namespace", "job:Apply", "", "/api/v1/definitions/jobs/foreign/task", `{"spec_json":"{}"}`, 403},
		{"foreign tenant", "job:Apply", "", "/api/v1/definitions/jobs/own/task?tenant_id=foreign", `{"spec_json":"{}"}`, 403},
		{"group refused", "job:Apply", "group", "/api/v1/definitions/jobs/own/task", `{"spec_json":"{}"}`, 403},
		{"unknown metadata refused", "job:Apply", "", "/api/v1/definitions/jobs/own/task", `{"spec_json":"{}","metadata":{"finalizers":["x"]}}`, 400},
		{"bounded request", "job:Apply", "", "/api/v1/definitions/jobs/own/task", `{"spec_json":"` + strings.Repeat("x", 270000) + `"}`, 400},
		{"independent apply", "job:Apply", "", "/api/v1/definitions/jobs/own/task", `{"spec_json":"{}","dry_run":true}`, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			h := NewHandler(nil, nil, nil)
			d := &fakeDefinitions{}
			h.SetDefinitionBackend(d)
			r := gin.New()
			r.Use(testPrincipalMiddleware(usecaseTestTenant, "key", test.group))
			r.Use(func(c *gin.Context) {
				middleware.SetDocuments(c, []policy.Document{{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{test.action}, Resource: []string{"orbitjob:*:*:*/*"}}}}})
			})
			h.Register(r)
			req := httptest.NewRequest("PUT", test.path, strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != test.status {
				t.Fatalf("HTTP %d wanted %d: %s", w.Code, test.status, w.Body.String())
			}
			if d.called != (test.status == 200) {
				t.Fatal("rejected request reached mutation")
			}
		})
	}
}
func TestLogRoutesAuthorizeActualFunctionParent(t *testing.T) {
	for _, test := range []struct {
		name, path, group, actual, action, arn string
		deny                                   bool
		status                                 int
	}{
		{"missing independent grant", "/api/v1/instances/11/attempts/1/logs", "", "", "instance:Get", "orbitjob:*:*:*/*", false, 403},
		{"group cannot tunnel instance", "/api/v1/instances/11/attempts/1/logs", "group", "group", "instance:ReadLogs", "orbitjob:*:*:*/*", false, 403},
		{"own grouped function", "/api/v1/functions/7/runs/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/attempts/1/logs", "group", "group", "function:ReadLogs", "orbitjob:*:group:function/7", false, 200},
		{"wrong grouped function", "/api/v1/functions/7/runs/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/attempts/1/logs", "group", "other", "function:ReadLogs", "orbitjob:*:*:*/*", false, 403},
		{"actual group deny", "/api/v1/functions/7/runs/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/attempts/1/logs", "", "group", "function:ReadLogs", "orbitjob:*:*:*/*", true, 403},
		{"no pod locator", "/api/v1/instances/11/attempts/1/logs?pod=foreign", "", "", "instance:ReadLogs", "orbitjob:*:*:*/*", false, 400},
		{"limits bound", "/api/v1/instances/11/attempts/1/logs?limit_bytes=65537", "", "", "instance:ReadLogs", "orbitjob:*:*:*/*", false, 400},
		{"own instance", "/api/v1/instances/11/attempts/1/logs", "", "", "instance:ReadLogs", "orbitjob:*:*:instance/11", false, 200},
		{"other exact instance", "/api/v1/instances/12/attempts/1/logs", "", "", "instance:ReadLogs", "orbitjob:*:*:instance/11", false, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			h := NewHandler(nil, nil, nil)
			reader := &fakeLogReader{group: test.actual}
			backend := &fakeLogBackend{}
			h.SetLogBackend(reader, backend)
			r := gin.New()
			r.Use(testPrincipalMiddleware(usecaseTestTenant, "key", test.group))
			r.Use(func(c *gin.Context) {
				statements := []policy.Statement{{Effect: policy.EffectAllow, Action: []string{test.action}, Resource: []string{test.arn}}}
				if test.deny {
					statements = append(statements, policy.Statement{Effect: policy.EffectDeny, Action: []string{"function:ReadLogs"}, Resource: []string{"orbitjob:*:group:function/7"}})
				}
				middleware.SetDocuments(c, []policy.Document{{Statement: statements}})
			})
			h.Register(r)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", test.path, nil))
			if w.Code != test.status {
				t.Fatalf("HTTP%d wanted%d: %s", w.Code, test.status, w.Body.String())
			}
			if backend.called != (test.status == 200) {
				t.Fatal("denied or invalid query reached logs")
			}
		})
	}
}
