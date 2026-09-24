package v2

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	root "github.com/sofiworker/gk/ghttp"
)

type comparisonInput struct {
	ID     int    `path:"id"`
	Page   int    `query:"page"`
	Filter string `query:"filter"`
}
type comparisonBody struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}
type comparisonOutput struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
type comparisonWriter struct{ header http.Header }

func (w *comparisonWriter) Header() http.Header       { return w.header }
func (*comparisonWriter) Write(p []byte) (int, error) { return len(p), nil }
func (*comparisonWriter) WriteHeader(int)             {}

// 两个版本复用同一路由器和请求构造策略；测量包含绑定、codec 与路由分发。
// Both versions share routing and request construction; measurements include binding, codecs and dispatch.
func comparisonServer(version, scenario string) (*root.Server, string, string, error) {
	s := root.New()
	method, path := http.MethodGet, "/health"
	var err error
	switch scenario {
	case "GetNone":
		h := func(context.Context) (comparisonOutput, error) { return comparisonOutput{ID: 1, Name: "ok"}, nil }
		if version == "v1" {
			err = root.GetNone(s, path, root.JSON[comparisonOutput](), h)
		} else {
			err = FromFunc(method, path, h).Mount(s)
		}
	case "GetParams":
		path = "/users/{id}"
		h := func(_ context.Context, in comparisonInput) (comparisonOutput, error) {
			return comparisonOutput{ID: in.ID, Name: in.Filter}, nil
		}
		if version == "v1" {
			err = root.GetParams(s, path, root.JSON[comparisonOutput](), h)
		} else if version == "v2DecodeWith" {
			err = Get(path, h, WithInput(DecodeWith(decodeComparisonParams))).Mount(s)
		} else if version == "v2DecodeWithFast" {
			err = Get(path, h, WithInput(DecodeWith(decodeComparisonParams)), WithUnsafeFastPath()).Mount(s)
		} else {
			err = Get(path, h).Mount(s)
		}
		path = "/users/7?page=2&filter=golang"
	case "PostBody":
		method, path = http.MethodPost, "/register"
		h := func(_ context.Context, in comparisonBody) (comparisonOutput, error) {
			return comparisonOutput{Name: in.Name}, nil
		}
		if version == "v1" {
			err = root.PostBody(s, path, root.JSONBody[comparisonBody](), root.JSON[comparisonOutput](), h)
		} else {
			err = Post(path, h).Mount(s)
		}
	}
	return s, method, path, err
}

func TestComparisonContracts(t *testing.T) {
	for _, scenario := range []string{"GetNone", "GetParams", "PostBody"} {
		var expected string
		for _, version := range []string{"v1", "v2", "v2DecodeWith", "v2DecodeWithFast"} {
			s, method, path, err := comparisonServer(version, scenario)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(method, path, nil)
			if method == http.MethodPost {
				req.Body = io.NopCloser(strings.NewReader(`{"name":"alice","email":"a@example.com","age":30}`))
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("%s %s: %d %s", version, scenario, rec.Code, rec.Body.String())
			}
			if version == "v1" {
				expected = rec.Body.String()
			} else if rec.Body.String() != expected {
				t.Fatalf("%s bodies differ: %q %q", scenario, expected, rec.Body.String())
			}
		}
	}
}

func BenchmarkV1V2(b *testing.B) {
	for _, scenario := range []string{"GetNone", "GetParams", "PostBody"} {
		b.Run(scenario, func(b *testing.B) {
			for _, version := range []string{"v1", "v2"} {
				b.Run(version, func(b *testing.B) {
					s, method, path, err := comparisonServer(version, scenario)
					if err != nil {
						b.Fatal(err)
					}
					req := httptest.NewRequest(method, path, nil)
					req.Header.Set("Content-Type", "application/json")
					writer := &comparisonWriter{header: make(http.Header)}
					payload := `{"name":"alice","email":"a@example.com","age":30}`
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if method == http.MethodPost {
							req.Body = io.NopCloser(strings.NewReader(payload))
						}
						s.ServeHTTP(writer, req)
					}
				})
			}
		})
	}
}

func BenchmarkV1V2Registration(b *testing.B) {
	for _, version := range []string{"v1", "v2"} {
		b.Run(version, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, _, err := comparisonServer(version, "GetParams"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// 手写字段赋值，但保留与自动绑定相同的 query 解析和整数转换。
// Assign fields directly while retaining the same query parsing and integer conversion.
func decodeComparisonParams(_ context.Context, req *Request) (comparisonInput, error) {
	var in comparisonInput
	var err error
	if raw := req.Params.Get("id"); raw != "" {
		in.ID, err = strconv.Atoi(raw)
		if err != nil {
			return in, err
		}
	}

	if value, ok := req.QueryFirst("page"); ok {
		in.Page, err = strconv.Atoi(value)
		if err != nil {
			return in, err
		}
	}
	if value, ok := req.QueryFirst("filter"); ok {
		in.Filter = value
	}
	return in, nil
}

func BenchmarkDecodeWithComparison(b *testing.B) {
	for _, version := range []string{"v1", "v2", "v2DecodeWith", "v2DecodeWithFast"} {
		b.Run(version, func(b *testing.B) {
			s, method, path, err := comparisonServer(version, "GetParams")
			if err != nil {
				b.Fatal(err)
			}
			req := httptest.NewRequest(method, path, nil)
			w := &comparisonWriter{header: make(http.Header)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.ServeHTTP(w, req)
			}
		})
	}
}

func requestViewServer(t testing.TB, mode string) *root.Server {
	t.Helper()
	s := root.New()
	h := func(_ context.Context, in comparisonInput) (comparisonInput, error) { return in, nil }
	var route Route
	switch mode {
	case "DTO":
		route = Get("/users/{id}", h)
	case "DecodeWith":
		route = Get("/users/{id}", h, WithInput(DecodeWith(decodeComparisonParams)))
	default:
		route = Get("/users/{id}", func(ctx context.Context, in RequestInput) (comparisonInput, error) {
			if mode == "Sources" {
				src := in.Sources()
				id, err := strconv.Atoi(src.Path("id"))
				if err != nil {
					return comparisonInput{}, err
				}
				raw, _ := src.QueryFirst("page")
				page, err := strconv.Atoi(raw)
				if err != nil {
					return comparisonInput{}, err
				}
				filter, _ := src.QueryFirst("filter")
				return comparisonInput{ID: id, Page: page, Filter: filter}, nil
			}
			if mode == "Map" {
				in.Query()
			}
			return decodeComparisonParams(ctx, in.Request)
		})
	}
	if err := route.Mount(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRequestViewComparison(t *testing.T) {
	for _, mode := range []string{"DTO", "DecodeWith", "RequestInput", "Sources", "Map"} {
		s := requestViewServer(t, mode)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", "/users/7?page=2&filter=golang", nil))
		if rec.Code != 200 || rec.Body.String() != "{\"ID\":7,\"Page\":2,\"Filter\":\"golang\"}\n" {
			t.Fatalf("%s: %d %s", mode, rec.Code, rec.Body.String())
		}
	}
}

func BenchmarkRequestInputComparison(b *testing.B) {
	for _, mode := range []string{"DTO", "DecodeWith", "RequestInput", "Sources", "Map"} {
		b.Run(mode, func(b *testing.B) {
			s := requestViewServer(b, mode)
			req := httptest.NewRequest("GET", "/users/7?page=2&filter=golang", nil)
			w := &comparisonWriter{header: make(http.Header)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				clear(w.header)
				s.ServeHTTP(w, req)
			}
		})
	}
}

func TestDecodeWithComparisonPage(t *testing.T) {
	in, err := decodeComparisonParams(context.Background(), &Request{Request: httptest.NewRequest("GET", "/?page=2&filter=golang", nil)})
	if err != nil || in.Page != 2 || in.Filter != "golang" {
		t.Fatalf("%+v %v", in, err)
	}
	_, err = decodeComparisonParams(context.Background(), &Request{Request: httptest.NewRequest("GET", "/?page=invalid", nil)})
	if err == nil {
		t.Fatal("invalid page accepted")
	}
}
