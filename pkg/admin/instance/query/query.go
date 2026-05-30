// Package query provides instance query use cases.
package query

import internal "orbitjob/internal/admin/app/instance/query"

type (
	ListInstancesUseCase = internal.ListInstancesUseCase
	ListInstancesInput   = internal.ListInstancesInput
	InstanceItem         = internal.InstanceItem
	GetInstanceUseCase   = internal.GetInstanceUseCase
)

var (
	NewListInstancesUseCase = internal.NewListInstancesUseCase
	NewGetInstanceUseCase   = internal.NewGetInstanceUseCase
)
