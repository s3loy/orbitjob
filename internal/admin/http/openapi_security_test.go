package http

import (
	"strings"
	"testing"
)

func TestOpenAPISecurityMatchesRouteAuthentication(t *testing.T) {
	doc := ServiceOpenAPIDocument()
	bearer := doc.Components.SecuritySchemes["APIKey"]
	if bearer.Type != "http" || bearer.Scheme != "bearer" || bearer.Description == "" {
		t.Fatalf("missing API key scheme: %+v", bearer)
	}
	cookie := doc.Components.SecuritySchemes["ConsoleSession"]
	if cookie.Type != "apiKey" || cookie.In != "cookie" || cookie.Name != "__Host-orbitjob_session" {
		t.Fatalf("missing session cookie scheme: %+v", cookie)
	}
	for path, item := range doc.Paths {
		for method, op := range map[string]*Operation{"GET": item.Get, "POST": item.Post, "PUT": item.Put, "PATCH": item.Patch, "DELETE": item.Delete} {
			if op == nil {
				continue
			}
			if path == "/healthz" || path == "/metrics" || path == "/openapi.json" {
				if len(op.Security) != 0 || op.Permission != "" {
					t.Fatalf("public %s %s requires credentials", method, path)
				}
				continue
			}
			if strings.HasPrefix(path, "/api/v1/auth/") {
				public := path == "/api/v1/auth/session" || path == "/api/v1/auth/login" || path == "/api/v1/auth/setup" || path == "/api/v1/auth/invitations/accept" || path == "/api/v1/auth/password/reset"
				if public {
					if len(op.Security) != 0 {
						t.Fatalf("credential entry %s requires prior auth", path)
					}
				} else if len(op.Security) != 1 {
					t.Fatalf("account operation %s lacks session auth", path)
				} else if _, ok := op.Security[0]["ConsoleSession"]; !ok {
					t.Fatalf("account operation %s lacks cookie scheme", path)
				}
				continue
			}
			identity := (path == "/api/v1/identity" || path == "/api/v1/workspaces") && method == "GET"
			if identity && op.Permission != "" {
				t.Fatalf("identity must not require an unrelated resource permission")
			}
			if len(op.Security) != 2 || (!identity && op.Permission == "") {
				t.Fatalf("%s %s lacks authentication/permission metadata", method, path)
			}
			if _, ok := op.Security[0]["APIKey"]; !ok {
				t.Fatalf("%s lacks bearer auth", path)
			}
			if _, ok := op.Security[1]["ConsoleSession"]; !ok {
				t.Fatalf("%s lacks cookie auth", path)
			}
		}
	}
}
