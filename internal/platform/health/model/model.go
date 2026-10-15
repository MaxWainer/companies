package model

import "time"

//go:generate go tool easyjson -disable_members_unescape model.go

type Status string //	@name	HealthStatus

const (
	StatusUp       Status = "up"
	StatusDegraded Status = "degraded"
	StatusDown     Status = "down"
)

//easyjson:json
type Report struct {
	Status Status            `json:"status"`
	Checks map[string]Result `json:"checks,omitempty"`
} //	@name	HealthReport

//easyjson:json
type Result struct {
	Status     Status            `json:"status"`
	Critical   bool              `json:"critical"`
	Error      string            `json:"error,omitempty"`
	DurationMS float64           `json:"duration_ms"`
	CheckedAt  time.Time         `json:"checked_at"`
	Details    map[string]string `json:"details,omitempty"`
} //	@name	HealthCheckResult
