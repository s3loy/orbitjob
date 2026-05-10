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
	c.Request = httptest.NewRequest("GET", "/", nil)
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
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Authorization", "Bearer "+key)

	auth.Middleware()(c)

	if len(c.Errors) == 0 {
		t.Fatalf("expected an error, got none")
	}
}

func TestAuth_FallbackHeader(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	auth := NewAuth(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("X-OrbitJob-Tenant-Id", "tenant-fallback")

	auth.Middleware()(c)

	tid, src := TenantID(c.Request.Context())
	if tid != "tenant-fallback" {
		t.Fatalf("expected tenant-fallback, got %q", tid)
	}
	if src != TenantSourceHeader {
		t.Fatalf("expected x-tenant-id source, got %q", src)
	}
}

func TestAuth_NoAuth_Default(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	auth := NewAuth(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	auth.Middleware()(c)

	tid, src := TenantID(c.Request.Context())
	if tid != "" {
		t.Fatalf("expected empty tenant_id, got %q", tid)
	}
	if src != "" {
		t.Fatalf("expected empty source, got %q", src)
	}
	if GetTenantID(c) != "default" {
		t.Fatalf("expected GetTenantID=default, got %q", GetTenantID(c))
	}
}

func TestGetTenantID(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	auth := NewAuth(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request = c.Request.WithContext(WithTenantID(context.Background(), "tc", "x-tenant-id"))

	auth.Middleware()(c)

	if GetTenantID(c) != "tc" {
		t.Fatalf("expected tc, got %q", GetTenantID(c))
	}
	if GetTenantSource(c) != "x-tenant-id" {
		t.Fatalf("expected x-tenant-id, got %q", GetTenantSource(c))
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
