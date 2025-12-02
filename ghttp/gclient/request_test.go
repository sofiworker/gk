package gclient

import "testing"

func TestReplacePathParams(t *testing.T) {
	path := replacePathParams("/api/:id/{name}", map[string]string{
		"id":   "42",
		"name": "foo bar",
	})
	if path != "/api/42/foo%20bar" {
		t.Fatalf("unexpected replaced path %s", path)
	}
}
