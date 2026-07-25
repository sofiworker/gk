package gerr

import (
	"fmt"
	"strings"
)

type Kind string

const (
	KindUnknown         Kind = ""
	KindInvalid         Kind = "invalid"
	KindNotFound        Kind = "not_found"
	KindConflict        Kind = "conflict"
	KindUnauthenticated Kind = "unauthenticated"
	KindPermission      Kind = "permission"
	KindRateLimited     Kind = "rate_limited"
	KindUnavailable     Kind = "unavailable"
	KindTimeout         Kind = "timeout"
	KindCanceled        Kind = "canceled"
	KindInternal        Kind = "internal"
)

type Error struct {
	ID      string
	Kind    Kind
	Params  map[string]any
	Meta    map[string]any
	Op      string
	Message string
	Err     error
}

type Option func(*Error)

func New(id string, kind Kind, opts ...Option) *Error {
	e := &Error{ID: id, Kind: kind}
	applyOptions(e, opts)
	return e
}

func Wrap(err error, opts ...Option) error {
	if err == nil {
		return nil
	}
	e := &Error{Err: err}
	applyOptions(e, opts)
	return e
}

func applyOptions(e *Error, opts []Option) {
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	e.Params = cloneStringMap(e.Params)
	e.Meta = cloneStringMap(e.Meta)
}

func WithID(id string) Option           { return func(e *Error) { e.ID = id } }
func WithKind(kind Kind) Option         { return func(e *Error) { e.Kind = kind } }
func WithOp(op string) Option           { return func(e *Error) { e.Op = op } }
func WithMessage(message string) Option { return func(e *Error) { e.Message = message } }
func WithCause(err error) Option        { return func(e *Error) { e.Err = err } }

func WithParam(key string, value any) Option {
	return func(e *Error) {
		if e.Params == nil {
			e.Params = make(map[string]any)
		}
		e.Params[key] = cloneValue(value)
	}
}

func WithParams(params map[string]any) Option {
	return func(e *Error) { e.Params = cloneStringMap(params) }
}

func WithMeta(key string, value any) Option {
	return func(e *Error) {
		if e.Meta == nil {
			e.Meta = make(map[string]any)
		}
		e.Meta[key] = cloneValue(value)
	}
}

func WithMetadata(meta map[string]any) Option {
	return func(e *Error) { e.Meta = cloneStringMap(meta) }
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	for _, text := range []string{e.Message, e.ID, string(e.Kind)} {
		if text != "" {
			b.WriteString(text)
			break
		}
	}
	if e.Err != nil {
		if b.Len() > 0 {
			b.WriteString(": ")
		}
		b.WriteString(e.Err.Error())
	}
	if b.Len() == 0 {
		return "error"
	}
	return b.String()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *Error) Is(target error) bool {
	if e == target {
		return true
	}
	t, ok := target.(*Error)
	if !ok || t == nil {
		return false
	}
	matched := false
	for _, comparison := range []struct {
		set   bool
		equal bool
	}{{t.ID != "", e.ID == t.ID}, {t.Kind != KindUnknown, e.Kind == t.Kind}, {t.Op != "", e.Op == t.Op}, {t.Message != "", e.Message == t.Message}} {
		if comparison.set {
			matched = true
			if !comparison.equal {
				return false
			}
		}
	}
	return matched
}

func (e *Error) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v', 's':
		_, _ = fmt.Fprint(s, e.Error())
	case 'q':
		_, _ = fmt.Fprintf(s, "%q", e.Error())
	}
}

func cloneStringMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneValue(value)
	}
	return cloned
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneStringMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i := range typed {
			cloned[i] = cloneValue(typed[i])
		}
		return cloned
	default:
		return value
	}
}
