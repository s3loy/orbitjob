package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/core/domain/policy"
)

// withDocs installs a principal and its policy documents, standing in for what
// the auth middleware does on a real request.
func withDocs(principal Principal, docs []policy.Document) gin.HandlerFunc {
	return func(c *gin.Context) {
		SetPrincipal(c, principal)
		SetDocuments(c, docs)
		c.Next()
	}
}

func jobARN(c *gin.Context) (policy.ARN, error) {
	return policy.ARN{Tenant: c.Param("tenant"), Group: c.Param("group"), Type: "job", ID: c.Param("id")}, nil
}

func TestRequireAllowsPermittedAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(withDocs(
		Principal{TenantID: "T1", Kind: KindTenant},
		[]policy.Document{{Statement: []policy.Statement{
			{Effect: policy.EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:T1:*:job/*"}},
		}}},
	))
	r.GET("/jobs/:tenant/:group/:id", Require("job:Get", jobARN), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/jobs/T1/ci/7", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequireRejectsUnpermittedAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(withDocs(
		Principal{TenantID: "T1", Kind: KindTenant},
		[]policy.Document{{Statement: []policy.Statement{
			{Effect: policy.EffectAllow, Action: []string{"job:List"}, Resource: []string{"orbitjob:T1:*:job/*"}},
		}}},
	))
	r.GET("/jobs/:tenant/:group/:id", Require("job:Get", jobARN), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/jobs/T1/ci/7", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

// A request whose target cannot be resolved must be denied, never allowed.
func TestRequireDeniesWhenResourceUnresolvable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(withDocs(
		Principal{TenantID: "T1", Kind: KindTenant},
		[]policy.Document{{Statement: []policy.Statement{
			{Effect: policy.EffectAllow, Action: []string{"job:Get"}, Resource: []string{"orbitjob:*:*:*/*"}},
		}}},
	))
	failing := func(*gin.Context) (policy.ARN, error) { return policy.ARN{}, errors.New("no target") }
	r.GET("/jobs/:id", Require("job:Get", failing), func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/jobs/7", nil))
	if w.Code == http.StatusOK {
		t.Fatal("an unresolvable target must not be allowed through")
	}
}

// Missing documents means the authorization context never got set up. That is a
// wiring defect, not a permission decision, so it must not silently allow.
func TestRequireDeniesWhenDocumentsMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		SetPrincipal(c, Principal{TenantID: "T1", Kind: KindTenant})
		c.Next()
	})
	r.GET("/jobs/:id", Require("job:Get", jobARN), func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/jobs/7", nil))
	if w.Code == http.StatusOK {
		t.Fatal("a missing authorization context must not allow the request")
	}
}

func TestPrincipalRoundTrips(t *testing.T) {
	gin.SetMode(gin.TestMode)
	want := Principal{
		KeyID:            "ak_1",
		TenantID:         "T1",
		Kind:             KindTenant,
		ResourceGroupID:  "g1",
		BoundaryPolicyID: "p1",
	}
	var (
		got    Principal
		gotOK  bool
		isPlat bool
	)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		SetPrincipal(c, want)
		c.Next()
	})
	r.GET("/x", func(c *gin.Context) {
		got, gotOK = PrincipalFrom(c)
		isPlat = got.IsPlatform()
		c.Status(http.StatusOK)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if !gotOK {
		t.Fatal("PrincipalFrom reported no principal after SetPrincipal")
	}
	if got != want {
		t.Fatalf("principal round-trip = %+v, want %+v", got, want)
	}
	if isPlat {
		t.Fatal("a tenant principal is not a platform principal")
	}
}

func TestPrincipalFromReportsMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var ok bool
	r := gin.New()
	r.GET("/x", func(c *gin.Context) {
		_, ok = PrincipalFrom(c)
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if ok {
		t.Fatal("PrincipalFrom should report false when nothing was set")
	}
}

func TestPlatformPrincipalIsPlatform(t *testing.T) {
	p := Principal{Kind: KindPlatform}
	if !p.IsPlatform() {
		t.Fatal("KindPlatform must report as a platform principal")
	}
}

// Documents must report absence distinctly from an empty grant list: the first
// is a wiring defect, the second is a legitimate denial.
func TestDocumentsDistinguishesUnsetFromEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var (
		unsetOK  bool
		emptyOK  bool
		emptyLen int
	)
	r := gin.New()
	r.GET("/unset", func(c *gin.Context) {
		_, unsetOK = Documents(c)
		c.Status(http.StatusOK)
	})
	r.GET("/empty", func(c *gin.Context) {
		SetDocuments(c, []policy.Document{})
		docs, ok := Documents(c)
		emptyOK, emptyLen = ok, len(docs)
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/unset", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/empty", nil))

	if unsetOK {
		t.Fatal("Documents must report false when nothing was set")
	}
	if !emptyOK || emptyLen != 0 {
		t.Fatalf("an explicit empty grant list must be reported as set: ok=%v len=%d", emptyOK, emptyLen)
	}
}
