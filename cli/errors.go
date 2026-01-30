package cli

import "fmt"

// Exit codes per specification (Section 14.4).
const (
	ExitSuccess       = 0 // Success
	ExitGeneral       = 1 // General/unknown error
	ExitConfig        = 2 // Invalid YAML, missing required config fields
	ExitDatabase      = 3 // Database init fails, corrupt/locked
	ExitAgentNotFound = 4 // Agent not found or not detected
	ExitHookFailed    = 5 // Hook installation/removal fails
)

// ExitCoder is an interface for errors that carry a custom exit code and message.
type ExitCoder interface {
	ExitCode() int
	Message() string
}

// CLIError is a typed error that carries an exit code.
type cliError struct {
	code    int
	message string
	err     error
}

// NewCLIError creates a new CLIError with the given code and message.
func NewCLIError(code int, message string) *cliError {
	return &cliError{
		code:    code,
		message: message,
	}
}

// WrapError creates a new CLIError wrapping an underlying error.
func WrapError(code int, message string, err error) *cliError {
	return &cliError{
		code:    code,
		message: message,
		err:     err,
	}
}

// Error implements the error interface.
func (e *cliError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %v", e.message, e.err)
	}
	return e.message
}

// ExitCode returns the exit code for this error.
func (e *cliError) ExitCode() int {
	return e.code
}

// Message returns the formatted message for display.
func (e *cliError) Message() string {
	return fmt.Sprintf("Error: %s\n", e.Error())
}

// Unwrap returns the underlying error for errors.Is/errors.As support.
func (e *cliError) Unwrap() error {
	return e.err
}

// ErrConfig creates a configuration error.
func ErrConfig(message string, err error) *cliError {
	return WrapError(ExitConfig, message, err)
}

// ErrDatabase creates a database error.
func ErrDatabase(message string, err error) *cliError {
	return WrapError(ExitDatabase, message, err)
}

// ErrAgentNotFound creates an agent not found error.
func ErrAgentNotFound(agentName string) *cliError {
	return NewCLIError(ExitAgentNotFound, fmt.Sprintf("agent not found: %s", agentName))
}

// ErrHookFailed creates a hook operation failure error.
func ErrHookFailed(message string, err error) *cliError {
	return WrapError(ExitHookFailed, message, err)
}
