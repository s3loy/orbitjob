package postgres

import (
	"errors"
	"net"
	"syscall"
	"testing"

	"github.com/lib/pq"

	domain "orbitjob/internal/core/domain"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		class domain.ErrorClass
	}{
		{"nil", nil, domain.ClassNone},
		{"unique violation", &pq.Error{Code: "23505"}, domain.SkipWorthy},
		{"serialization failure", &pq.Error{Code: "40001"}, domain.SkipWorthy},
		{"lock not available", &pq.Error{Code: "55P03"}, domain.SkipWorthy},
		{"deadlock", &pq.Error{Code: "40P01"}, domain.BackoffWorthy},
		{"query canceled", &pq.Error{Code: "57014"}, domain.BackoffWorthy},
		{"cannot connect now", &pq.Error{Code: "57P03"}, domain.BackoffWorthy},
		{"too many connections", &pq.Error{Code: "53300"}, domain.FatalWorthy},
		{"disk full", &pq.Error{Code: "53100"}, domain.FatalWorthy},
		{"admin shutdown", &pq.Error{Code: "57P01"}, domain.FatalWorthy},
		{"crash shutdown", &pq.Error{Code: "57P02"}, domain.FatalWorthy},
		{"out of memory", &pq.Error{Code: "53P00"}, domain.FatalWorthy},
		{"connection failure", &pq.Error{Code: "08006"}, domain.FatalWorthy},
		{"unable to connect", &pq.Error{Code: "08001"}, domain.FatalWorthy},
		{"connection refused op", &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "db"}}, domain.FatalWorthy},
		{"connection reset syscall", syscall.ECONNREFUSED, domain.FatalWorthy},
		{"unknown pq code", &pq.Error{Code: "99999"}, domain.BackoffWorthy},
		{"plain error", errors.New("something went wrong"), domain.BackoffWorthy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyError(tt.err); got != tt.class {
				t.Errorf("ClassifyError() = %v, want %v", got, tt.class)
			}
		})
	}
}
