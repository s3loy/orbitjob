package postgres

import "database/sql"

// nullableGroup converts an empty resource group id into SQL NULL.
//
// The list queries take the caller's group as a parameter and disable the
// filter when it is NULL. Passing "" instead would compare against the empty
// string, which matches no row -- turning "unscoped caller" into "caller who
// sees nothing", a silent outage rather than an open filter.
func nullableGroup(groupID string) any {
	if groupID == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: groupID, Valid: true}
}
