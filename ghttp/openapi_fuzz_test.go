package ghttp

import (
	"context"
	"testing"
)

// FuzzOpenAPIRouteDocumentation exercises document generation with arbitrary
// user supplied documentation strings and visibility settings.
func FuzzOpenAPIRouteDocumentation(f *testing.F) {
	for _, seed := range []string{"", "summary", "quotes \" and newline\n", "中文", "<script>"} {
		f.Add(seed, false)
		f.Add(seed, true)
	}

	f.Fuzz(func(t *testing.T, text string, hidden bool) {
		s := NewServer()
		route := Get("/fuzz/:id", func(context.Context, RequestOf[NoDataType]) (struct {
			OK bool `json:"ok"`
		}, error) {
			return struct {
				OK bool `json:"ok"`
			}{OK: true}, nil
		}, WithDoc(text, text), WithOpenAPIExpose(!hidden))
		if err := s.Register(route); err != nil {
			t.Fatalf("register route: %v", err)
		}
		doc, err := OpenAPIDocument(s)
		if err != nil {
			t.Fatalf("generate document: %v", err)
		}
		paths, ok := doc["paths"].(map[string]any)
		if !ok {
			t.Fatalf("paths has type %T", doc["paths"])
		}
		_, present := paths["/fuzz/{id}"]
		if present == hidden {
			t.Fatalf("hidden=%v, path present=%v", hidden, present)
		}
	})
}
