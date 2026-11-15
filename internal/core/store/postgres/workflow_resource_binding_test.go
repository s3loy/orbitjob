package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"orbitjob/internal/domain/resource"
)

func TestWorkflowResourceBindingIsWriteOnceAndAudited(t *testing.T) {
	for _, current := range []string{"", "original-uid", "replacement-uid"} {
		t.Run("current_"+current, func(t *testing.T) {
			repo, mock := newWorkflowRepoMock(t)
			expectWorkflowTx(mock, "tenant-a")
			mock.ExpectQuery("SELECT resource_uid, actor").WithArgs("tenant-a", int64(9)).WillReturnRows(sqlmock.NewRows([]string{"resource_uid", "actor"}).AddRow(current, "key-1"))
			switch current {
			case "":
				mock.ExpectExec("UPDATE workflow_run_control_plane SET resource_uid").WithArgs("original-uid", "tenant-a", int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
				expectControlPlaneAudit(mock, "key-1")
				mock.ExpectCommit()
			case "original-uid":
				mock.ExpectCommit()
			case "replacement-uid":
				mock.ExpectRollback()
			}
			err := repo.BindResource(context.Background(), "tenant-a", 9, "original-uid")
			if current == "replacement-uid" {
				var conflict *resource.ConflictError
				if !errors.As(err, &conflict) {
					t.Fatalf("replacement binding err=%v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
