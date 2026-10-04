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
// it to generate with. Every enabled API key of that channel is evaluated.
type IntelligenceTarget struct {
	ChannelID int    `json:"channelId"`
	ModelID   string `json:"modelId"`
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
}
