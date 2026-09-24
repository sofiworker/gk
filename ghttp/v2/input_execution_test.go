package v2

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestDecodeWithContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "value")
	r := Get("/", func(_ context.Context, in *contractInput) (string, error) { return in.Name, nil }, WithInput(DecodeWith(func(c context.Context, _ *Request) (*contractInput, error) {
		return &contractInput{Name: c.Value(key{}).(string)}, nil
	})))
	rec := httptest.NewRecorder()
	if err := r.Serve(ctx, &Request{Request: httptest.NewRequest("GET", "/", nil)}, &Response{ResponseWriter: rec}); err != nil {
		t.Fatal(err)
	}
	if rec.Body.String() != "\"value\"\n" {
		t.Fatal(rec.Body.String())
	}
}

func TestCompiledInputIsolation(t *testing.T) {
	type input struct {
		Page int `query:"page"`
	}
	decoder, err := defaultInput[*input]()
	if err != nil {
		t.Fatal(err)
	}
	read, err := compileInput(decoder)
	if err != nil {
		t.Fatal(err)
	}
	const count = 32
	results := make([]*input, count)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, e := read(context.Background(), &Request{Request: httptest.NewRequest("GET", "/?page=7", nil)})
			if e != nil {
				t.Error(e)
				return
			}
			v.Page += i
			results[i] = v
		}(i)
	}
	wg.Wait()
	seen := map[*input]bool{}
	for i, v := range results {
		if v == nil || v.Page != 7+i || seen[v] {
			t.Fatalf("shared or invalid input at %d", i)
		}
		seen[v] = true
	}
}

func TestSourceTypeRejectedAtRegistration(t *testing.T) {
	type input struct {
		Value map[string]string `query:"value"`
	}
	if err := Get("/", func(context.Context, input) (string, error) { return "", nil }).Err(); err == nil {
		t.Fatal("unsupported source type accepted")
	}
}
