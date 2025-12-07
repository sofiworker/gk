package gclient

import (
	"testing"
)

func TestConstructURL(t *testing.T) {
	// Valid
	u, err := ConstructURL("http://base.com", "/path")
	if err != nil { t.Fatal(err) }
	if u != "http://base.com/path" { t.Errorf("got %s", u) }

	// Valid Abs
	u, err = ConstructURL("", "http://abs.com/path")
	if err != nil { t.Fatal(err) }
	if u != "http://abs.com/path" { t.Errorf("got %s", u) }

	// Invalid Path
	_, err = ConstructURL("http://base.com", ":/bad")
	if err == nil { t.Error("expected error for bad path") }

	// Empty Base
	_, err = ConstructURL("", "/path")
	if err == nil { t.Error("expected error for empty base") }

	// Bad Base
	_, err = ConstructURL(":/bad", "/path")
	if err == nil { t.Error("expected error for bad base") }
}

func TestValidMethod(t *testing.T) {
	if !ValidMethod("GET") { t.Error("GET invalid") }
	if ValidMethod("") { t.Error("empty valid") }
	if ValidMethod("GET /") { t.Error("space valid") }
}

func TestIsValidURL(t *testing.T) {
	if !IsValidURL("http://google.com") { t.Error("valid url failed") }
	// url.Parse is very lenient, so almost anything is valid except control chars
}
