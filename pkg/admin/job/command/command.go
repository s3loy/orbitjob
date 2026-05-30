// Package command provides job command use cases.
package command

import internalcmd "orbitjob/internal/admin/app/job/command"

type (
	CreateJobUseCase    = internalcmd.CreateJobUseCase
	CreateInput         = internalcmd.CreateInput
	CreateResult        = internalcmd.CreateResult
	UpdateJobUseCase    = internalcmd.UpdateJobUseCase
	UpdateInput         = internalcmd.UpdateInput
	UpdateResult        = internalcmd.UpdateResult
	ChangeStatusUseCase = internalcmd.ChangeStatusUseCase
	ChangeStatusInput   = internalcmd.ChangeStatusInput
	ChangeStatusResult  = internalcmd.ChangeStatusResult
	DeleteJobUseCase    = internalcmd.DeleteJobUseCase
	DeleteInput         = internalcmd.DeleteInput
	DeleteResult        = internalcmd.DeleteResult
	TriggerJobUseCase   = internalcmd.TriggerJobUseCase
	TriggerInput        = internalcmd.TriggerInput
	TriggerResult       = internalcmd.TriggerResult
)

var (
	NewCreateJobUseCase    = internalcmd.NewCreateJobUseCase
	NewUpdateJobUseCase    = internalcmd.NewUpdateJobUseCase
	NewChangeStatusUseCase = internalcmd.NewChangeStatusUseCase
	NewDeleteJobUseCase    = internalcmd.NewDeleteJobUseCase
	NewTriggerJobUseCase   = internalcmd.NewTriggerJobUseCase
)
