// Package platformctl implements the SteadyState developer CLI.
package platformctl

import (
	"errors"
	"fmt"
	"strings"
)

const (
	ConfigAPIVersion  = "cli.steadystate.dev/v1alpha1"
	ConfigKind        = "Config"
	CatalogAPIVersion = "cli.steadystate.dev/v1alpha1"
	CatalogKind       = "TenantCatalog"
)

const (
	ExitUsage     = 2
	ExitAuth      = 3
	ExitNotFound  = 4
	ExitUnhealthy = 5
	ExitConflict  = 6
	ExitTimeout   = 7
	ExitRemote    = 8
)

// ExitError carries the stable process exit code for a command failure.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

func exitError(code int, format string, args ...any) error {
	return &ExitError{Code: code, Err: fmt.Errorf(format, args...)}
}

// ExitCode returns the public exit code represented by err.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var coded *ExitError
	if errors.As(err, &coded) {
		return coded.Code
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"unknown command", "unknown flag", "requires at least", "requires exactly", "accepts ", "arg(s)"} {
		if strings.Contains(message, marker) {
			return ExitUsage
		}
	}
	return ExitRemote
}
