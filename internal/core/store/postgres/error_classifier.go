package postgres

import (
	"errors"
	"net"
	"syscall"

	"github.com/lib/pq"

	domain "orbitjob/internal/core/domain"
)

// ClassifyError maps a database or network error to its adaptive control category.
// nil returns ClassNone. Unknown errors default to BackoffWorthy (safe: they count
// toward breaker but don't immediately open it).
func ClassifyError(err error) domain.ErrorClass {
	if err == nil {
		return domain.ClassNone
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return domain.BackoffWorthy
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return domain.BackoffWorthy
	}

	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Code {
		case "23505", "40001", "55P03":
			return domain.SkipWorthy
		case "40P01", "57014", "57P03":
			return domain.BackoffWorthy
		case "08001", "08006", "53300", "53100", "57P01", "57P02", "53P00":
			return domain.FatalWorthy
		}
	}

	return domain.BackoffWorthy
}
