package jobapp

import (
	"context"
	"orbitjob/internal/job"
	"time"
)

type testRepo struct {
	called bool
	in     job.CreateJobSpec
	out    job.Job
	err    error
}

func (r *testRepo) Create(ctx context.Context, in job.CreateJobSpec) (job.Job, error) {
	r.called = true
	r.in = in
	return r.out, r.err
}

type fixedClock struct {
	t time.Time
}

func (c fixedClock) Now() time.Time {
	return c.t
}
