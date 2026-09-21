package check

const (
	// TenantIDLength is the exact width of a tenant id. Every tenant_id column
	// is CHAR(26) holding a ULID, and a check whose tenant does not match that
	// shape could never address its own rows, so the boundary rejects anything
	// else rather than normalizing it into a fallback.
	TenantIDLength    = 26
	DefaultTimezone   = "UTC"
	DefaultTimeoutSec = 30
	DefaultRetryLimit = 2
	DefaultPriority   = 5

	ScheduleTypeCron     = "cron"
	ScheduleTypeInterval = "interval"

	CheckTypeHTTPHealth = "http_health"

	StatusActive = "active"
	StatusPaused = "paused"

	ActionPause  = "pause"
	ActionResume = "resume"
)

// Probe rendering bounds for http_health checks. A check executes as a
// Kubernetes Job running a curl-semantics container, so the configuration
// fields below are exactly what the rendering consumes; validating them at the
// admin boundary keeps a misrendered probe from being discoverable only by
// watching a Job fail.
const (
	// MinimumIntervalSec is the fastest cadence an interval check may run at.
	// Every occurrence is its own Kubernetes Job, and a one-second probe would
	// spend the API server and the scheduler queue on work nobody reads. The
	// database CHECK keeps its old looser bound; this floor is boundary
	// validation, not schema.
	MinimumIntervalSec = 30

	// ProbeMethods are the HTTP methods a probe may use. v1 probes are
	// exit-code-only and must stay side-effect-free: GET, HEAD and OPTIONS are
	// the methods whose contract says the request observes rather than mutates.
	// Methods with payloads (POST/PUT) belong with the assertion engine that
	// can also evaluate their responses, not with a bare exit code.
	DefaultProbeMethod = "GET"

	// ExpectedStatus bounds are the HTTP response-code space; anything outside
	// it can never match what a server sends.
	DefaultExpectedStatus = 200
	MinExpectedStatus     = 100
	MaxExpectedStatus     = 599

	// MaxProbeURLLength bounds the probe target. The URL rides the Job's
	// container args and every audit/ledger text that quotes the spec; an
	// unbounded one lets one check definition carry megabytes of query string
	// through all of them.
	MaxProbeURLLength = 2048
)

// ProbeMethods is the exact set the boundary admits. It is a slice rather than
// a const because Go has no constant arrays.
var ProbeMethods = []string{"GET", "HEAD", "OPTIONS"}

// CreateInput is the domain input for check creation.
type CreateInput struct {
	Name        string
	Description *string
	TenantID    string
	// ResourceGroupID is the creating key's own scope.
	ResourceGroupID string
	CheckType       string
	CheckConfig     map[string]any
	AssertionRules  []AssertionRule
	ScheduleType    string
	CronExpr        *string
	IntervalSec     *int
	Timezone        string
	TimeoutSec      int
	RetryLimit      int
	Priority        int
	Labels          map[string]any
}

// AssertionRule defines a single evaluation rule for the evaluator.
type AssertionRule struct {
	Metric    string  `json:"metric"`
	Operator  string  `json:"operator"`
	Threshold float64 `json:"threshold"`
	Severity  string  `json:"severity"`
}
