//go:build integration

package postgrestest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestWaitForWaitingLockAcceptsObservationWithinContextBudget(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, waiting := range []bool{false, true} {
		expect := mock.ExpectQuery("SELECT EXISTS").
			WithArgs(expectedLockClassID, expectedLockObjectID).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(waiting))
		if waiting {
			// Exceed the former per-query deadline while remaining inside the
			// same overall observation budget used by the real database test.
			expect.WillDelayFor(650 * time.Millisecond)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if waiting, err := waitForWaitingLock(ctx, db, expectedLockClassID, expectedLockObjectID); err != nil || !waiting {
		t.Fatalf("in-budget waiting observation = %v, %v", waiting, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForWaitingLockPreservesQueryFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	want := errors.New("observer unavailable")
	mock.ExpectQuery("SELECT EXISTS").WithArgs(expectedLockClassID, expectedLockObjectID).WillReturnError(want)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if waiting, err := waitForWaitingLock(ctx, db, expectedLockClassID, expectedLockObjectID); waiting || !errors.Is(err, want) {
		t.Fatalf("failed observation = %v, %v", waiting, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForWaitingLockDeadlineDoesNotConfirmAnAbsentLock(t *testing.T) {
	db := openTestDB(t, packageTestDSN(t), "observer without lock holder")
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if waiting, err := waitForWaitingLock(ctx, db, expectedLockClassID, expectedLockObjectID); waiting || err == nil {
		t.Fatalf("unobserved lock at deadline = %v, %v", waiting, err)
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("observation ended before its deadline: %v", ctx.Err())
	}
}

func TestWaitForWaitingLockRequiresLiveContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waiting, err := waitForWaitingLock(ctx, nil, expectedLockClassID, expectedLockObjectID); waiting || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled observation = %v, %v", waiting, err)
	}
}
