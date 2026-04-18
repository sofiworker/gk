package gws

import (
	"errors"
	"strings"
)

var ErrFaultNotFound = errors.New("SOAP fault not found")

func extractFault(data []byte) (*Fault, error) {
	env, err := unmarshalEnvelope(data)
	if err != nil {
		return nil, err
	}

	if env.Body.Fault == nil {
		return nil, ErrFaultNotFound
	}

	fault := &Fault{
		Code:   env.Body.Fault.Code,
		String: env.Body.Fault.String,
		Actor:  env.Body.Fault.Actor,
	}

	if env.Body.Fault.Detail != nil {
		detail := strings.TrimSpace(env.Body.Fault.Detail.InnerXML)
		if detail != "" {
			fault.Detail = detail
		}
	}

	return fault, nil
}
