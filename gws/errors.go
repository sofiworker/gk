package gws

import "fmt"

type Fault struct {
	Code   string
	String string
	Actor  string
	Detail any
}

type FaultError struct {
	StatusCode int
	Fault      Fault
}

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
