package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	query "orbitjob/internal/admin/app/workspace/query"
	"orbitjob/internal/core/domain/policy"
)

func TestWorkspaceDirectoryRepositoryUsesComputedScopeAndDoesNotHideReadErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo := NewTenantRepository(db)
	input := query.DirectoryInput{Scope: policy.TenantScope{All: true, Exclude: []string{"hidden"}}, Search: "%_\\", Limit: 21, Offset: 20}
	mock.ExpectQuery(`FROM orbitjob_workspace_directory`).WithArgs(true, nil, `{"hidden"}`, input.Search, 21, 20).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "status"}).AddRow("visible", "Human name", "human-name", "active"))
	rows, err := repo.ListWorkspaceDirectory(context.Background(), input)
	if err != nil || len(rows) != 1 || rows[0].Name != "Human name" {
		t.Fatalf("metadata rows=%+v err=%v", rows, err)
	}
	mock.ExpectQuery(`FROM orbitjob_workspace_directory`).WillReturnError(errors.New("directory unavailable"))
	if _, err := repo.ListWorkspaceDirectory(context.Background(), input); err == nil {
		t.Fatal("directory error became empty success")
	}
	mock.ExpectQuery(`FROM orbitjob_workspace_directory`).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "status"}).AddRow(nil, "bad", "bad", "active"))
	if _, err := repo.ListWorkspaceDirectory(context.Background(), input); err == nil {
		t.Fatal("invalid metadata was silently returned")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
