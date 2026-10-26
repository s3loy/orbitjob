package bootstrap

import (
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"golang.org/x/crypto/bcrypt"
	"testing"
)

func TestEnsureDefaultRetriesSecretDeliveryAfterPartialSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	hash, err := bcrypt.GenerateFromPassword([]byte(DefaultAPIKey), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, created := range []bool{true, false} {
		mock.ExpectBegin()
		mock.ExpectQuery("orbitjob_bootstrap_default").WillReturnRows(sqlmock.NewRows([]string{"tenant_created", "key_created"}).AddRow(created, created))
		mock.ExpectCommit()
	}
	mock.ExpectQuery("SELECT key_hash FROM orbitjob_auth_api_key").WithArgs(DefaultAPIKey[:12], DefaultAPIKeyID).WillReturnRows(sqlmock.NewRows([]string{"key_hash"}).AddRow(string(hash)))
	writer := &fakeSecretWriter{err: errors.New("temporary delivery failure")}
	if _, err := EnsureDefault(t.Context(), db, Options{Writer: writer}); err == nil {
		t.Fatal("expected delivery failure")
	}
	writer.err = nil
	if _, err := EnsureDefault(t.Context(), db, Options{Writer: writer}); err != nil {
		t.Fatal(err)
	}
	if len(writer.calls) != 2 {
		t.Fatalf("delivery attempts = %d, want 2", len(writer.calls))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureDefaultNeverDeliversAnUnmatchedExistingKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	hash, err := bcrypt.GenerateFromPassword([]byte(DefaultAPIKey), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectQuery("orbitjob_bootstrap_default").WillReturnRows(sqlmock.NewRows([]string{"tenant_created", "key_created"}).AddRow(false, false))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT key_hash FROM orbitjob_auth_api_key").WithArgs(DefaultAPIKey[:12], DefaultAPIKeyID).WillReturnRows(sqlmock.NewRows([]string{"key_hash"}).AddRow(string(hash)))
	writer := &fakeSecretWriter{}
	if _, err := EnsureDefault(t.Context(), db, Options{APIKey: DefaultAPIKey + "different", Writer: writer}); err == nil {
		t.Fatal("mismatched plaintext accepted")
	}
	if len(writer.calls) != 0 {
		t.Fatal("mismatched plaintext was delivered")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
