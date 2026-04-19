package gws

import "fmt"

// Fault is the logical SOAP fault model exposed to callers and service
// implementations.
type Fault struct {
	Code   string
	String string
	Actor  string
	Detail any
}

// FaultError wraps a SOAP fault returned from a remote endpoint together with
// the HTTP status code when available.
type FaultError struct {
	StatusCode int
	Fault      Fault
}

// Error implements the error interface.
func (e *FaultError) Error() string {
	if e == nil {
		return "soap fault: <nil>"
	}

	if e.StatusCode > 0 {
		return fmt.Sprintf(
			"soap fault (status=%d, code=%s): %s",
			e.StatusCode,
			e.Fault.Code,
			e.Fault.String,
		)
	}

	return fmt.Sprintf("soap fault (code=%s): %s", e.Fault.Code, e.Fault.String)
}
