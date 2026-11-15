package postgres

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/domain/resource"
	"orbitjob/internal/platform/scan"
)

func TestSLOCreateRejectsUnavailableScopedSLI(t *testing.T) {
	repo, mock := newSLORepoMock(t)
	tenant, group := scan.GenerateID(), scan.GenerateID()
	mock.ExpectBegin()
	expectSetConfig(mock, tenant)
	mock.ExpectQuery("SELECT id FROM slis").WithArgs(tenant, int64(9), group).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	_, err := repo.Create(context.Background(), tenant, slo.CreateSpec{SLIID: 9, ResourceGroupID: group})
	var missing *resource.NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("error=%v, want scoped not-found", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSLICreateRejectsUnavailableScopedSource(t *testing.T) {
	for _, source := range []string{"check-9", "scheduled-uid"} {
		t.Run(source, func(t *testing.T) {
			repo, mock := newSLIRepoMock(t)
			tenant, group := scan.GenerateID(), scan.GenerateID()
			mock.ExpectBegin()
			expectSetConfig(mock, tenant)
			mock.ExpectExec(`SELECT orbitjob_lock_sli_source\(\$1,\$2\)`).
				WithArgs(tenant, source).WillReturnResult(sqlmock.NewResult(0, 0))
			if source == "check-9" {
				mock.ExpectQuery("SELECT id FROM checks").WithArgs(tenant, int64(9), group).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			} else {
				mock.ExpectQuery("SELECT id FROM job_definition_revisions").WithArgs(tenant, source, group).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			}
			mock.ExpectRollback()
			_, err := repo.Create(context.Background(), tenant, sli.CreateSpec{SourceType: "job_run", SourceConfig: map[string]any{"source_uid": source}, ResourceGroupID: group})
			var missing *resource.NotFoundError
			if !errors.As(err, &missing) {
				t.Fatalf("error=%v, want scoped not-found", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
