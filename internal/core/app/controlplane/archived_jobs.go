package controlplane

type ArchivedJob struct {
	RunID      int64
	Namespace  string
	JobName    string
	JobUID     string
	RunName    string
	RunUID     string
	RevisionID int64
	Attempt    int
}
