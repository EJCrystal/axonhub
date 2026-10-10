package objects

// IntelligenceConfig is the persisted configuration of the scheduled
// intelligence check. It lives in the system key-value table rather than its own
// table because it is a single system-wide setting, like the retry policy.
type IntelligenceConfig struct {
	// Enabled turns the scheduled run on and off. Manual runs are always
	// available.
	Enabled bool `json:"enabled"`

	// IntervalMinutes is the fixed-rate interval between runs.
	IntervalMinutes int `json:"intervalMinutes"`

	// Targets lists the channel/model pairs to evaluate.
	Targets []IntelligenceTarget `json:"targets"`
}

// IntelligenceTarget is one channel paired with the model the check should ask
// it to generate with.
//
// APIKey narrows the run to a single key, which is what the settings screen
// offers: a model is only meaningful for the key that can actually serve it.
// An empty APIKey keeps the older behaviour of evaluating every enabled key.
type IntelligenceTarget struct {
	ChannelID int    `json:"channelId"`
	ModelID   string `json:"modelId"`
	APIKey    string `json:"apiKey,omitempty"`
	// ReasoningEffort overrides the model's thinking level for this target. An
	// empty value leaves the request untouched, so the provider default applies.
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	// TimeoutMinutes overrides how long this target may spend on one run. Zero
	// keeps the built-in default, which suits every channel but the slowest: an
	// upstream that regularly needs over ten minutes to finish the HTML needs a
	// longer budget than the rest of the configuration.
	TimeoutMinutes int `json:"timeoutMinutes,omitempty"`
	// Benchmark selects which check this target runs. Empty means the pelican
	// (鹈鹕骑行) HTML benchmark, which is what every stored target used before the
	// field existed; the candy (糖果) question is the alternative. An unknown value
	// is rejected on save rather than silently falling back, so a typo cannot look
	// like a check that passed.
	Benchmark string `json:"benchmark,omitempty"`
}

// IntelligenceRunStatus is the terminal state of one scheduled or manual run.
type IntelligenceRunStatus string

const (
	// IntelligenceRunRunning is the state a run holds while the check is in
	// flight, so the history can show it without waiting for the result.
	IntelligenceRunRunning IntelligenceRunStatus = "running"

	IntelligenceRunSucceeded IntelligenceRunStatus = "succeeded"
	IntelligenceRunFailed    IntelligenceRunStatus = "failed"
	IntelligenceRunPartial   IntelligenceRunStatus = "partial"
)

// IntelligenceRunTerminalStatuses are the states a finished run can hold.
var IntelligenceRunTerminalStatuses = []IntelligenceRunStatus{
	IntelligenceRunSucceeded,
	IntelligenceRunFailed,
	IntelligenceRunPartial,
}

// IntelligenceKeyResult is the outcome for a single API key within a run.
type IntelligenceKeyResult struct {
	KeyPrefix string `json:"keyPrefix"`
	Success   bool   `json:"success"`
	Quality   string `json:"quality"`
	Label     string `json:"label"`
	Reason    string `json:"reason"`
	TaskID    string `json:"taskId"`
	// GenerationMs is how long the tested model took to produce the source.
	GenerationMs int `json:"generationMs"`
	// DurationMs is the whole run for this key: generation plus submission and
	// polling.
	DurationMs int    `json:"durationMs"`
	HTML       string `json:"html"`
	// Answer is the text the candy question produced. It is kept apart from HTML
	// so the history can render each benchmark the way it reads: a page for
	// pelican, plain text for candy.
	Answer string  `json:"answer,omitempty"`
	Error  *string `json:"error,omitempty"`
	// ManualVerdict is an operator's decision when automatic scoring produced no
	// usable answer, e.g. because the detection service could not be reached. It
	// takes precedence over Quality so the recorded outcome matches what a human
	// actually judged, and stays empty until someone sets it.
	ManualVerdict string `json:"manualVerdict,omitempty"`
}

// Manual verdicts an operator can record for one key.
const (
	IntelligenceManualVerdictNormal   = "normal"
	IntelligenceManualVerdictDegraded = "degraded"
)
