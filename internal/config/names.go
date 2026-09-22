package config

import (
	"fmt"
	"regexp"
)

// validNamePattern is the allowed pattern for newly created server names.
// Existing names that don't match remain visible and must not be silently
// renamed or removed; ValidateNewName is not applied on load.
var validNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidateNewName reports whether name is allowed for a server created or
// renamed through mcpctl. It must not be used to reject names already
// present in a loaded project file.
func ValidateNewName(name string) error {
	if name == "" {
		return fmt.Errorf("server name must not be empty")
	}
	if !validNamePattern.MatchString(name) {
		return fmt.Errorf("server name %q must match %s", name, validNamePattern.String())
	}
	return nil
}
