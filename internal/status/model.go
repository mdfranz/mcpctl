// Package status turns claude/codex/opencode CLI output into a per-server,
// per-client Result with independent status dimensions: configuration
// presence, live connectivity, and authentication are never conflated.
package status

import "time"

type ConfigState string

const (
	ConfigPresent         ConfigState = "present"
	ConfigMissing         ConfigState = "missing"
	ConfigDisabled        ConfigState = "disabled"
	ConfigPendingApproval ConfigState = "pending_approval"
	ConfigRejected        ConfigState = "rejected"
	ConfigUnknown         ConfigState = "unknown"
)

type ConnectionState string

const (
	ConnectionConnected ConnectionState = "connected"
	ConnectionFailed    ConnectionState = "failed"
	ConnectionTimedOut  ConnectionState = "timed_out"
	ConnectionUnchecked ConnectionState = "unchecked"
)

type AuthState string

const (
	AuthAuthenticated AuthState = "authenticated"
	AuthRequired      AuthState = "required"
	AuthNotRequired   AuthState = "not_required"
	AuthUnknown       AuthState = "unknown"
)

type AuthMethod string

const (
	AuthMethodOAuth   AuthMethod = "oauth"
	AuthMethodBearer  AuthMethod = "bearer"
	AuthMethodHeaders AuthMethod = "headers"
	AuthMethodNone    AuthMethod = "none"
	AuthMethodUnknown AuthMethod = "unknown"
)

// CheckState reports whether this Result's other dimensions were
// actually determined, distinct from what they were determined to be.
// Incomplete checks take precedence over diagnosed issues: a caller must
// not treat CheckUnavailable/CheckFailed/CheckTimedOut/CheckCancelled
// output as if it were CheckComplete with unfavorable values.
type CheckState string

const (
	CheckComplete    CheckState = "complete"
	CheckUnavailable CheckState = "unavailable"
	CheckUnsupported CheckState = "unsupported"
	CheckTimedOut    CheckState = "timed_out"
	CheckFailed      CheckState = "failed"
	CheckCancelled   CheckState = "cancelled"
)

type Scope string

const (
	ScopeProject Scope = "project"
	ScopeOther   Scope = "other"
	ScopeUnknown Scope = "unknown"
)

// Evidence is one redacted, bounded record of a command run to produce
// (part of) a Result.
type Evidence struct {
	Command  []string
	ExitCode int
	Summary  string
	Duration time.Duration
}

// Result is one server's status as seen by one client. Config presence,
// live connectivity, and authentication are independent: a present
// config with a token reference is not proof of a working authenticated
// connection, and a connected session may still be anonymous.
type Result struct {
	ServerName    string
	Client        string
	ClientVersion string
	Target        string // observed URL or stdio command, when exposed

	ConfigState ConfigState
	Connection  ConnectionState
	AuthState   AuthState
	AuthMethod  AuthMethod
	CheckState  CheckState
	Scope       Scope

	CheckedAt time.Time
	Duration  time.Duration
	Evidence  []Evidence
}
