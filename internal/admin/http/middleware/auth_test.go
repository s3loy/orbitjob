package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"golang.org/x/crypto/bcrypt"
)

func TestAuth_BearerToken_Valid(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	key := "otj_a1b2c3d4e5f6g7h8"
	hash, _ := bcrypt.GenerateFromPassword([]byte(key), bcrypt.MinCost)

	mock.ExpectQuery("SELECT ak.id, ak.tenant_id, ak.key_hash").
		WithArgs("otj_a1b2c3d4").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "key_hash", "revoked", "expired"}).
			AddRow("ak_001", "tenant-42", string(hash), nil, nil))

	auth := NewAuth(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/jobs", nil)
	c.Request.Header.Set("Authorization", "Bearer "+key)

	auth.Middleware()(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	tid, src := TenantID(c.Request.Context())
	if tid != "tenant-42" {
		t.Fatalf("expected tenant-42, got %q", tid)
	}
	if src != TenantSourceAPIKey {
		t.Fatalf("expected api_key source, got %q", src)
	}
}

func TestAuth_BearerToken_Invalid(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	key := "otj_badkey0000000000"

	mock.ExpectQuery("SELECT ak.id, ak.tenant_id, ak.key_hash").
		WithArgs("otj_badkey00").
		WillReturnError(sqlmock.ErrCancelled)

	auth := NewAuth(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/jobs", nil)
	c.Request.Header.Set("Authorization", "Bearer "+key)

	auth.Middleware()(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if !c.IsAborted() {
		t.Fatal("expected request to be aborted")
	}
}

func TestAuth_NoToken_PrivatePath_401(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	auth := NewAuth(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/jobs", nil)

	auth.Middleware()(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if !c.IsAborted() {
		t.Fatal("expected request to be aborted")
	}
}

func TestAuth_NoToken_PublicPath_Allowed(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	auth := NewAuth(db)

	for _, path := range []string{"/healthz", "/openapi.json", "/metrics"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", path, nil)

		auth.Middleware()(c)

		if c.IsAborted() {
			t.Fatalf("expected %s to be public, but request was aborted", path)
		}
	}
}

func TestAuth_HeaderFallback_Disabled(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	auth := NewAuth(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/jobs", nil)
	c.Request.Header.Set("X-OrbitJob-Tenant-Id", "tenant-fallback")

	auth.Middleware()(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when only header is provided, got %d", w.Code)
	}
	if !c.IsAborted() {
		t.Fatal("expected request to be aborted")
	}
}

// --- validateAPIKey edge cases ---

func TestValidateAPIKey_NilDB(t *testing.T) {
	auth := &Auth{DB: nil}
	_, ok := auth.validateAPIKey(context.Background(), "otj_a1b2c3d4e5f6g7h8")
	if ok {
		t.Fatal("expected false when DB is nil")
	}
}

func TestValidateAPIKey_MissingPrefix(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	auth := NewAuth(db)
	_, ok := auth.validateAPIKey(context.Background(), "badprefix_a1b2c")
	if ok {
		t.Fatal("expected false for key without otj_ prefix")
	}
}

func TestValidateAPIKey_TooShort(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	auth := NewAuth(db)
	_, ok := auth.validateAPIKey(context.Background(), "otj_short")
	if ok {
		t.Fatal("expected false for key shorter than 12 chars")
	}
}

func TestValidateAPIKey_BcryptMismatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	key := "otj_a1b2c3d4e5f6g7h8"
	otherKey := "otj_0000000000000000"
	hash, _ := bcrypt.GenerateFromPassword([]byte(otherKey), bcrypt.MinCost)

	mock.ExpectQuery("SELECT ak.id, ak.tenant_id, ak.key_hash").
		WithArgs("otj_a1b2c3d4").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "key_hash", "revoked", "expired"}).
			AddRow("ak_001", "tenant-42", string(hash), nil, nil))

	auth := NewAuth(db)
	_, ok := auth.validateAPIKey(context.Background(), key)
	if ok {
		t.Fatal("expected false for bcrypt mismatch")
	}
}

func TestValidateAPIKey_Revoked(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	key := "otj_a1b2c3d4e5f6g7h8"
	hash, _ := bcrypt.GenerateFromPassword([]byte(key), bcrypt.MinCost)
	revoked := "2026-01-01"

	mock.ExpectQuery("SELECT ak.id, ak.tenant_id, ak.key_hash").
		WithArgs("otj_a1b2c3d4").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "key_hash", "revoked", "expired"}).
			AddRow("ak_001", "tenant-42", string(hash), &revoked, nil))

	auth := NewAuth(db)
	_, ok := auth.validateAPIKey(context.Background(), key)
	if ok {
		t.Fatal("expected false for revoked key")
	}
}

func TestValidateAPIKey_Expired(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	key := "otj_a1b2c3d4e5f6g7h8"
	hash, _ := bcrypt.GenerateFromPassword([]byte(key), bcrypt.MinCost)
	expired := "2020-01-01"

	mock.ExpectQuery("SELECT ak.id, ak.tenant_id, ak.key_hash").
		WithArgs("otj_a1b2c3d4").
		WillReturnRows(sqlmock.NewRows([]string{"id", "tenant_id", "key_hash", "revoked", "expired"}).
			AddRow("ak_001", "tenant-42", string(hash), nil, &expired))

	auth := NewAuth(db)
	_, ok := auth.validateAPIKey(context.Background(), key)
	if ok {
		t.Fatal("expected false for expired key")
	}
}
