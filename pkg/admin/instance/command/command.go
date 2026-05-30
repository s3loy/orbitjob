// Package command provides instance command use cases.
package command

import internal "orbitjob/internal/admin/app/instance/command"

type (
	CancelInstanceUseCase = internal.CancelInstanceUseCase
	CancelInstanceInput   = internal.CancelInstanceInput
)

var NewCancelInstanceUseCase = internal.NewCancelInstanceUseCase
