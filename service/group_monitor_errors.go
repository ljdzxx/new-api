package service

import (
	"errors"
	"fmt"
)

// Public results contain stable codes only; causes stay in backend diagnostics.
type monitorFailure struct {
	code  string
	cause error
}

func (e *monitorFailure) Error() string           { return fmt.Sprintf("%s: %v", e.code, e.cause) }
func (e *monitorFailure) Unwrap() error           { return e.cause }
func monitorError(code string, cause error) error { return &monitorFailure{code: code, cause: cause} }
func monitorErrorCode(err error, fallback string) string {
	var failure *monitorFailure
	if errors.As(err, &failure) {
		return failure.code
	}
	return fallback
}
