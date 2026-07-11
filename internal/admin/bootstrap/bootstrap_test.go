package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestEnsureDefault_Skip(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	res, err := EnsureDefault(t.Context(), db, Options{Skip: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TenantCreated || res.KeyCreated || res.MaskedKey != "" {
		t.Fatalf("expected empty result, got %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnsureDefault_FirstRun(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO tenants").
		WithArgs(DefaultTenantID, defaultTenantSlug, defaultTenantName, defaultTenantStatus).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultTenantID))
	mock.ExpectQuery("INSERT INTO api_keys").
		WithArgs(DefaultAPIKeyID, DefaultTenantID, sqlmock.AnyArg(), DefaultAPIKey[:12], defaultAPIKeyPermissions).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultAPIKeyID))
	mock.ExpectCommit()

	res, err := EnsureDefault(t.Context(), db, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.TenantCreated {
		t.Fatalf("expected tenant created")
	}
	if !res.KeyCreated {
		t.Fatalf("expected key created")
	}
	if res.MaskedKey != DefaultAPIKey[:12]+"..." {
		t.Fatalf("unexpected masked key: %s", res.MaskedKey)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnsureDefault_TenantExistsKeyNew(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO tenants").
		WithArgs(DefaultTenantID, defaultTenantSlug, defaultTenantName, defaultTenantStatus).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("INSERT INTO api_keys").
		WithArgs(DefaultAPIKeyID, DefaultTenantID, sqlmock.AnyArg(), DefaultAPIKey[:12], defaultAPIKeyPermissions).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultAPIKeyID))
	mock.ExpectCommit()

	res, err := EnsureDefault(t.Context(), db, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TenantCreated {
		t.Fatalf("expected tenant not created")
	}
	if !res.KeyCreated {
		t.Fatalf("expected key created")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnsureDefault_BothExist(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO tenants").
		WithArgs(DefaultTenantID, defaultTenantSlug, defaultTenantName, defaultTenantStatus).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("INSERT INTO api_keys").
		WithArgs(DefaultAPIKeyID, DefaultTenantID, sqlmock.AnyArg(), DefaultAPIKey[:12], defaultAPIKeyPermissions).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectCommit()

	res, err := EnsureDefault(t.Context(), db, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TenantCreated || res.KeyCreated {
		t.Fatalf("expected nothing created, got %+v", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnsureDefault_CustomKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	customKey := "otj_customkey_42"
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO tenants").
		WithArgs(DefaultTenantID, defaultTenantSlug, defaultTenantName, defaultTenantStatus).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultTenantID))
	mock.ExpectQuery("INSERT INTO api_keys").
		WithArgs(DefaultAPIKeyID, DefaultTenantID, sqlmock.AnyArg(), customKey[:12], defaultAPIKeyPermissions).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultAPIKeyID))
	mock.ExpectCommit()

	res, err := EnsureDefault(t.Context(), db, Options{APIKey: customKey})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.MaskedKey != customKey[:12]+"..." {
		t.Fatalf("unexpected masked key: %s", res.MaskedKey)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnsureDefault_KeyRequiredWhenDefaultNotAllowed(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	_, err = EnsureDefault(t.Context(), db, Options{DisallowDefaultKey: true})
	if err == nil {
		t.Fatal("expected error when default key is not allowed and no key provided")
	}
}

func TestEnsureDefault_KeyTooShort(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	_, err = EnsureDefault(t.Context(), db, Options{APIKey: "short"})
	if err == nil {
		t.Fatal("expected error for short key, got nil")
	}
}

func TestEnsureDefault_HashError(t *testing.T) {
	original := hashPasswordFn
	hashPasswordFn = func(_ []byte, _ int) ([]byte, error) {
		return nil, errors.New("hash failed")
	}
	defer func() { hashPasswordFn = original }()

	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	_, err = EnsureDefault(t.Context(), db, Options{})
	if err == nil {
		t.Fatal("expected error from bcrypt, got nil")
	}
}

func TestEnsureDefault_BeginError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin().WillReturnError(errors.New("begin failed"))

	_, err = EnsureDefault(t.Context(), db, Options{})
	if err == nil {
		t.Fatal("expected error from begin, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnsureDefault_TenantQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO tenants").
		WithArgs(DefaultTenantID, defaultTenantSlug, defaultTenantName, defaultTenantStatus).
		WillReturnError(errors.New("tenant insert failed"))
	mock.ExpectRollback()

	_, err = EnsureDefault(t.Context(), db, Options{})
	if err == nil {
		t.Fatal("expected error from tenant insert, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnsureDefault_KeyQueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO tenants").
		WithArgs(DefaultTenantID, defaultTenantSlug, defaultTenantName, defaultTenantStatus).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultTenantID))
	mock.ExpectQuery("INSERT INTO api_keys").
		WithArgs(DefaultAPIKeyID, DefaultTenantID, sqlmock.AnyArg(), DefaultAPIKey[:12], defaultAPIKeyPermissions).
		WillReturnError(errors.New("key insert failed"))
	mock.ExpectRollback()

	_, err = EnsureDefault(t.Context(), db, Options{})
	if err == nil {
		t.Fatal("expected error from api key insert, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnsureDefault_CommitError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO tenants").
		WithArgs(DefaultTenantID, defaultTenantSlug, defaultTenantName, defaultTenantStatus).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultTenantID))
	mock.ExpectQuery("INSERT INTO api_keys").
		WithArgs(DefaultAPIKeyID, DefaultTenantID, sqlmock.AnyArg(), DefaultAPIKey[:12], defaultAPIKeyPermissions).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultAPIKeyID))
	mock.ExpectCommit().WillReturnError(errors.New("commit failed"))

	_, err = EnsureDefault(t.Context(), db, Options{})
	if err == nil {
		t.Fatal("expected error from commit, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMaskKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"otj_devkey_2026", "otj_devkey_2..."},
		{"short", "..."},
		{"exactlytwelv", "..."},
	}
	for _, c := range cases {
		got := maskKey(c.in)
		if got != c.want {
			t.Errorf("maskKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

type fakeSecretWriter struct {
	calls []fakeSecretWriterCall
	err   error
}

type fakeSecretWriterCall struct {
	name string
	data map[string]string
}

func (f *fakeSecretWriter) Write(ctx context.Context, name string, data map[string]string) error {
	f.calls = append(f.calls, fakeSecretWriterCall{name: name, data: data})
	return f.err
}

func TestEnsureDefault_WritesSecretWhenKeyCreated(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO tenants").
		WithArgs(DefaultTenantID, defaultTenantSlug, defaultTenantName, defaultTenantStatus).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultTenantID))
	mock.ExpectQuery("INSERT INTO api_keys").
		WithArgs(DefaultAPIKeyID, DefaultTenantID, sqlmock.AnyArg(), DefaultAPIKey[:12], defaultAPIKeyPermissions).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultAPIKeyID))
	mock.ExpectCommit()

	writer := &fakeSecretWriter{}
	res, err := EnsureDefault(t.Context(), db, Options{Writer: writer})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.KeyCreated {
		t.Fatal("expected key created")
	}
	if len(writer.calls) != 1 {
		t.Fatalf("expected 1 writer call, got %d", len(writer.calls))
	}
	if writer.calls[0].name != defaultSecretName {
		t.Fatalf("unexpected secret name: %s", writer.calls[0].name)
	}
	if writer.calls[0].data[defaultSecretDataKey] != DefaultAPIKey {
		t.Fatalf("unexpected secret data")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnsureDefault_SkipsSecretWhenKeyExists(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO tenants").
		WithArgs(DefaultTenantID, defaultTenantSlug, defaultTenantName, defaultTenantStatus).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultTenantID))
	mock.ExpectQuery("INSERT INTO api_keys").
		WithArgs(DefaultAPIKeyID, DefaultTenantID, sqlmock.AnyArg(), DefaultAPIKey[:12], defaultAPIKeyPermissions).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectCommit()

	writer := &fakeSecretWriter{}
	res, err := EnsureDefault(t.Context(), db, Options{Writer: writer})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.KeyCreated {
		t.Fatal("expected key not created")
	}
	if len(writer.calls) != 0 {
		t.Fatalf("expected no writer calls, got %d", len(writer.calls))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnsureDefault_WriterError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new mock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO tenants").
		WithArgs(DefaultTenantID, defaultTenantSlug, defaultTenantName, defaultTenantStatus).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultTenantID))
	mock.ExpectQuery("INSERT INTO api_keys").
		WithArgs(DefaultAPIKeyID, DefaultTenantID, sqlmock.AnyArg(), DefaultAPIKey[:12], defaultAPIKeyPermissions).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(DefaultAPIKeyID))
	mock.ExpectCommit()

	writer := &fakeSecretWriter{err: errors.New("write failed")}
	_, err = EnsureDefault(t.Context(), db, Options{Writer: writer})
	if err == nil {
		t.Fatal("expected error from writer, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
