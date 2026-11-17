package ghttp

import "testing"

// mustPanics reports whether fn rejects a value through the Must* contract.
func mustPanics(fn func()) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	fn()
	return false
}

// FuzzValueMustConversions checks that every Must* accessor has exactly the
// same acceptance boundary as its error-returning counterpart.
func FuzzValueMustConversions(f *testing.F) {
	for _, seed := range []string{"", "0", "42", "-7", "3.14", "true", "false", "NaN", "invalid", " 42 "} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		v := newNamedValue("value", input, true)
		checks := []struct {
			name string
			err  error
			must func()
		}{
			{"string", func() error { _, err := v.String(); return err }(), func() { _ = v.MustString() }},
			{"int", func() error { _, err := v.Int(); return err }(), func() { _ = v.MustInt() }},
			{"int64", func() error { _, err := v.Int64(); return err }(), func() { _ = v.MustInt64() }},
			{"float64", func() error { _, err := v.Float64(); return err }(), func() { _ = v.MustFloat64() }},
			{"bool", func() error { _, err := v.Bool(); return err }(), func() { _ = v.MustBool() }},
		}
		for _, check := range checks {
			panicked := mustPanics(check.must)
			if want := check.err != nil; panicked != want {
				t.Fatalf("Must%s panic=%v, error=%v for input %q", check.name, panicked, check.err, input)
			}
		}
	})
}
