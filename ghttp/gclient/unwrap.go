package gclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

type ResponseUnwrapper func(*Response, interface{}) error

type ResponseStatusChecker func(*Response) error

type JSONEnvelopeConfig struct {
	DataField    string
	CodeField    string
	MessageField string
	SuccessCodes []interface{}
}

func (c *Client) SetResponseUnwrapper(fn ResponseUnwrapper) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responseUnwrapper = fn
	return c
}

func (c *Client) SetResponseStatusChecker(fn ResponseStatusChecker) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responseStatusChecker = fn
	return c
}

func (r *Request) SetResponseUnwrapper(fn ResponseUnwrapper) *Request {
	r.responseUnwrapper = fn
	return r
}

func (r *Request) SetResponseStatusChecker(fn ResponseStatusChecker) *Request {
	r.responseStatusChecker = fn
	return r
}

func JSONEnvelopeUnwrapper(config JSONEnvelopeConfig) ResponseUnwrapper {
	config = config.withDefaults()
	return func(resp *Response, out interface{}) error {
		if out == nil {
			return nil
		}
		fields, err := decodeEnvelopeFields(resp)
		if err != nil {
			return err
		}
		raw, ok := fields[config.DataField]
		if !ok {
			return ErrEnvelopeDataFieldNotFound
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		return decoder.Decode(out)
	}
}

func JSONEnvelopeStatusChecker(config JSONEnvelopeConfig) ResponseStatusChecker {
	config = config.withDefaults()
	return func(resp *Response) error {
		fields, err := decodeEnvelopeFields(resp)
		if err != nil {
			return err
		}

		rawCode, ok := fields[config.CodeField]
		if !ok {
			return nil
		}

		code, err := decodeEnvelopeValue(rawCode)
		if err != nil {
			return err
		}
		if containsEnvelopeValue(config.SuccessCodes, code) {
			return nil
		}

		msg := ""
		if rawMsg, ok := fields[config.MessageField]; ok {
			msg, _ = decodeEnvelopeMessage(rawMsg)
		}

		return &BusinessError{
			Code:     code,
			Message:  msg,
			Response: resp,
		}
	}
}

func JSONEnvelopeHandlers(config JSONEnvelopeConfig) (ResponseUnwrapper, ResponseStatusChecker) {
	return JSONEnvelopeUnwrapper(config), JSONEnvelopeStatusChecker(config)
}

func (c JSONEnvelopeConfig) withDefaults() JSONEnvelopeConfig {
	if c.DataField == "" {
		c.DataField = "data"
	}
	if c.CodeField == "" {
		c.CodeField = "code"
	}
	if c.MessageField == "" {
		c.MessageField = "msg"
	}
	if len(c.SuccessCodes) == 0 {
		c.SuccessCodes = []interface{}{0}
	}
	return c
}

func decodeEnvelopeFields(resp *Response) (map[string]json.RawMessage, error) {
	fields := make(map[string]json.RawMessage)
	if resp == nil || len(resp.Body) == 0 {
		return fields, nil
	}
	if err := json.Unmarshal(resp.Body, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func decodeEnvelopeValue(raw json.RawMessage) (interface{}, error) {
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return normalizeEnvelopeValue(value), nil
}

func decodeEnvelopeMessage(raw json.RawMessage) (string, error) {
	var msg string
	if err := json.Unmarshal(raw, &msg); err == nil {
		return msg, nil
	}
	value, err := decodeEnvelopeValue(raw)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(value), nil
}

func containsEnvelopeValue(successCodes []interface{}, value interface{}) bool {
	value = normalizeEnvelopeValue(value)
	for _, item := range successCodes {
		if reflect.DeepEqual(normalizeEnvelopeValue(item), value) {
			return true
		}
	}
	return false
}

func normalizeEnvelopeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case float64:
		if float64(int64(v)) == v {
			return int64(v)
		}
		return v
	case float32:
		if float32(int64(v)) == v {
			return int64(v)
		}
		return v
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case uint:
		return uint64(v)
	case uint8:
		return uint64(v)
	case uint16:
		return uint64(v)
	case uint32:
		return uint64(v)
	default:
		return value
	}
}
