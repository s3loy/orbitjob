// Package query provides job query use cases.
package query

import internal "orbitjob/internal/admin/app/job/query"

type (
	ListJobsUseCase = internal.ListJobsUseCase
	ListInput       = internal.ListInput
	ListItem        = internal.ListItem
	GetJobUseCase   = internal.GetJobUseCase
	GetInput        = internal.GetInput
	GetItem         = internal.GetItem
)

var (
	NewListJobsUseCase   = internal.NewListJobsUseCase
	NormalizeListInput   = internal.NormalizeListInput
	NewGetJobUseCase     = internal.NewGetJobUseCase
	NormalizeGetInput    = internal.NormalizeGetInput
	BuildScheduleSummary = internal.BuildScheduleSummary
)
