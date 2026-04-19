package gws

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrFaultNotFound indicates that a SOAP envelope does not contain a Fault
// element.
var ErrFaultNotFound = errors.New("SOAP fault not found")

// ExtractFault extracts a logical SOAP fault from raw SOAP response XML.
func ExtractFault(data []byte) (*Fault, error) {
	env, err := UnmarshalEnvelope(data)
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

// DecodeBodyPayload extracts the first payload element from a SOAP body.
func DecodeBodyPayload(data []byte) ([]byte, xml.Name, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, xml.Name{}, nil
	}

	var env responseEnvelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return nil, xml.Name{}, err
	}

	payload := strings.TrimSpace(env.Body.InnerXML)
	if payload == "" {
		return nil, xml.Name{}, nil
	}

	name, err := firstElementName([]byte(payload))
	if err != nil {
		return nil, xml.Name{}, err
	}

	return []byte(payload), name, nil
}

// UnmarshalBody decodes a SOAP body payload into out and optionally validates the wrapper.
func UnmarshalBody(data []byte, expectWrapper xml.Name, out any) error {
	payload, actualWrapper, err := DecodeBodyPayload(data)
	if err != nil {
		return err
	}

	if err := checkResponseWrapper(expectWrapper, actualWrapper); err != nil {
		return err
	}

	if len(payload) == 0 {
		return nil
	}

	return xml.Unmarshal(payload, out)
}

func firstElementName(data []byte) (xml.Name, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return xml.Name{}, nil
			}
			return xml.Name{}, err
		}

		if start, ok := token.(xml.StartElement); ok {
			return start.Name, nil
		}
	}
}

func checkResponseWrapper(expectWrapper, actualWrapper xml.Name) error {
	if isZeroXMLName(expectWrapper) {
		return nil
	}

	if expectWrapper == actualWrapper {
		return nil
	}

	return fmt.Errorf(
		"%w: want=%s got=%s",
		ErrResponseWrapperMismatch,
		formatXMLName(expectWrapper),
		formatXMLName(actualWrapper),
	)
}

func isZeroXMLName(name xml.Name) bool {
	return name.Local == "" && name.Space == ""
}

func formatXMLName(name xml.Name) string {
	if isZeroXMLName(name) {
		return "<empty>"
	}
	if name.Space == "" {
		return name.Local
	}
	return fmt.Sprintf("{%s}%s", name.Space, name.Local)
}
