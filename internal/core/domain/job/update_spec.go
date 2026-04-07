package job

// UpdateSpec is the normalized update input persisted by the write side.
type UpdateSpec struct {
	ID      int64
	Version int
	CreateSpec
}
