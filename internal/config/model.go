// Package config holds the canonical MCP server model shared across
// Claude Code, Codex CLI, and OpenCode project configuration formats.
package config

// ClientKind identifies a supported MCP client whose project config
// mcpctl reads and writes.
type ClientKind string

const (
	ClientClaude   ClientKind = "claude"
	ClientOpenCode ClientKind = "opencode"
	ClientCodex    ClientKind = "codex"
)

// ServerType distinguishes how a server is launched.
type ServerType string

const (
	ServerTypeStdio  ServerType = "stdio"
	ServerTypeRemote ServerType = "remote"
)

// EnvVarKind distinguishes a literal value from a passthrough reference
// to the user's environment.
type EnvVarKind string

const (
	EnvVarLiteral     EnvVarKind = "literal"
	EnvVarPassthrough EnvVarKind = "passthrough"
)

// EnvVar is one environment variable attached to a stdio server.
// Ordered: Env is a slice, not a map, so rendering stays deterministic.
type EnvVar struct {
	Name string
	Kind EnvVarKind
	// Value holds the literal value when Kind == EnvVarLiteral.
	Value string
	// Default holds the passthrough default when Kind == EnvVarPassthrough.
	// nil means no default; distinct from a present-but-empty default.
	Default *string
}

// HeaderValue is one HTTP header attached to a remote server.
type HeaderValue struct {
	Kind EnvVarKind
	// Value holds the literal value when Kind == EnvVarLiteral.
	Value string
	// EnvName holds the source environment variable when Kind == EnvVarPassthrough.
	EnvName string
}

// Server is the canonical, client-agnostic definition of one MCP server.
// Per-client extras that aren't modeled here (timeouts, OAuth options,
// enabled state, tool filters, ...) are tracked separately so they are
// never lost or copied between clients.
type Server struct {
	// Name must match ValidNamePattern for newly created servers.
	// Existing names that predate that rule are preserved as-is.
	Name string

	Type ServerType

	// Stdio fields.
	Command string
	Args    []string
	Env     []EnvVar

	// Remote fields.
	URL       string
	Transport string // preserves HTTP/SSE distinctions
	Headers   map[string]HeaderValue

	// BearerTokenEnvVar names the environment variable holding a bearer
	// token. It is never a resolved token value.
	BearerTokenEnvVar string
}
