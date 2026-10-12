// Package workerhealth defines bounded, non-financial worker diagnostics.
package workerhealth

type Queue struct {
	Kind, Status     string
	Count, Due       int64
	ExpiredLeases    int64
	OldestDueSeconds float64
}

type Snapshot struct {
	Queues        []Queue
	SourceEnabled *bool
	NAVStates     map[string]int64
	FXStates      map[string]int64
}
