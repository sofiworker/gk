package gerr

import "regexp"

var messageIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}(\.[a-z][a-z0-9_]{0,31}){1,7}$`)

type Descriptor struct {
	ID     string
	Kind   Kind
	Params map[string]any
}

type Describer interface {
	ErrorDescriptor() Descriptor
}

func ValidMessageID(id string) bool {
	return len(id) <= 128 && messageIDPattern.MatchString(id)
}

func (e *Error) ErrorDescriptor() Descriptor {
	if e == nil {
		return Descriptor{}
	}
	return Descriptor{ID: e.ID, Kind: e.Kind, Params: cloneStringMap(e.Params)}
}

func Describe(err error) (Descriptor, bool) {
	for err != nil {
		if describer, ok := err.(Describer); ok {
			descriptor := describer.ErrorDescriptor()
			if ValidMessageID(descriptor.ID) && descriptor.Kind != KindUnknown {
				descriptor.Params = cloneStringMap(descriptor.Params)
				return descriptor, true
			}
		}
		if _, ok := err.(unwrapMany); ok {
			return Descriptor{}, false
		}
		one, ok := err.(unwrapOne)
		if !ok {
			return Descriptor{}, false
		}
		err = one.Unwrap()
	}
	return Descriptor{}, false
}
