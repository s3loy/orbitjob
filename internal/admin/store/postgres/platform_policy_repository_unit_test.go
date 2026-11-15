package postgres

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPlatformDocumentsParseFailureIsNotAnUnboundedGrant(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectQuery(`SELECT policy_id, document, is_boundary FROM orbitjob_platform_key_documents`).WithArgs("authenticated").WillReturnRows(sqlmock.NewRows([]string{"policy_id", "document", "is_boundary"}).AddRow("grant", `{"statement":[{"effect":"Allow","action":["*"],"resource":["orbitjob:*:*:*/*"]}]}`, false).AddRow("invalid-boundary", `null`, true))
	docs, err := NewPolicyRepository(db).EffectivePlatformDocuments(context.Background(), "authenticated")
	if err == nil || len(docs) != 0 {
		t.Fatalf("invalid boundary yielded docs=%d err=%v", len(docs), err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
