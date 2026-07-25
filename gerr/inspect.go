package gerr

import "errors"

type unwrapOne interface {
	Unwrap() error
}

type unwrapMany interface {
	Unwrap() []error
}

func Is(err, target error) bool {
	return errors.Is(err, target)
}

func IsAny(err error, targets ...error) bool {
	for _, target := range targets {
		if target != nil && errors.Is(err, target) {
			return true
		}
	}
	return false
}

func IsAll(err error, targets ...error) bool {
	if len(targets) == 0 {
		return false
	}
	for _, target := range targets {
		if target == nil || !errors.Is(err, target) {
			return false
		}
	}
	return true
}

func As(err error, target interface{}) bool {
	return errors.As(err, target)
}

func AsType[T error](err error) (T, bool) {
	var target T
	if errors.As(err, &target) {
		return target, true
	}
	return target, false
}

func ErrorOf(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func IsID(err error, id string) bool {
	if id == "" {
		return false
	}
	return errors.Is(err, &Error{ID: id})
}

func IsKind(err error, kind Kind) bool {
	if kind == "" {
		return false
	}
	return errors.Is(err, &Error{Kind: kind})
}

func Contains(err error, match func(error) bool) bool {
	if err == nil || match == nil {
		return false
	}

	found := false
	Walk(err, func(candidate error) bool {
		if match(candidate) {
			found = true
			return false
		}
		return true
	})
	return found
}

func Walk(err error, visit func(error) bool) {
	if err == nil || visit == nil {
		return
	}

	var walk func(error) bool
	walk = func(candidate error) bool {
		if candidate == nil {
			return true
		}
		if !visit(candidate) {
			return false
		}
		if many, ok := candidate.(unwrapMany); ok {
			for _, child := range many.Unwrap() {
				if !walk(child) {
					return false
				}
			}
			return true
		}
		if one, ok := candidate.(unwrapOne); ok {
			return walk(one.Unwrap())
		}
		return true
	}
	_ = walk(err)
}

func Flatten(err error) []error {
	if err == nil {
		return nil
	}

	var out []error
	var flatten func(error)
	flatten = func(candidate error) {
		if candidate == nil {
			return
		}
		if many, ok := candidate.(unwrapMany); ok {
			children := many.Unwrap()
			if len(children) > 0 {
				for _, child := range children {
					flatten(child)
				}
				return
			}
		}
		if one, ok := candidate.(unwrapOne); ok {
			child := one.Unwrap()
			if child != nil {
				flatten(child)
				return
			}
		}
		out = append(out, candidate)
	}
	flatten(err)
	return out
}
