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
}

// IntelligenceRunStatus is the terminal state of one scheduled or manual run.
type IntelligenceRunStatus string

const (
	IntelligenceRunSucceeded IntelligenceRunStatus = "succeeded"
	IntelligenceRunFailed    IntelligenceRunStatus = "failed"
	IntelligenceRunPartial   IntelligenceRunStatus = "partial"
)

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
	DurationMs int     `json:"durationMs"`
	HTML       string  `json:"html"`
	Error      *string `json:"error,omitempty"`
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
