package ghttp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// formLevel 是实现 TextUnmarshaler 的测试类型。
// formLevel is a TextUnmarshaler test type.
type formLevel int

func (l *formLevel) UnmarshalText(b []byte) error {
	switch string(b) {
	case "low":
		*l = 1
	case "high":
		*l = 2
	default:
		return errors.New("bad level")
	}
	return nil
}

type formBase struct {
	ID int `form:"id"`
}

type formAll struct {
	formBase
	S     string                  `form:"s"`
	B     bool                    `form:"b"`
	I     int                     `form:"i"`
	I8    int8                    `form:"i8"`
	U     uint                    `form:"u"`
	U16   uint16                  `form:"u16"`
	F     float64                 `form:"f"`
	F32   float32                 `form:"f32"`
	T     time.Time               `form:"t"`
	Lvl   formLevel               `form:"lvl"`
	P     *int                    `form:"p"`
	PS    *string                 `form:"ps"`
	Tags  []string                `form:"tags"`
	Nums  []int                   `form:"nums"`
	Lvls  []formLevel             `form:"lvls"`
	NoTag string                  // 以字段名映射 / mapped by field name
	Skip  string                  `form:"-"`
	File  *multipart.FileHeader   `form:"file"`
	Files []*multipart.FileHeader `form:"files"`
}

func formReq(ct, body string) *Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	return &Request{Raw: r}
}

const formURLEnc = "application/x-www-form-urlencoded"

// formMultipart 构造 multipart 请求体，返回 Content-Type 与 body。
// formMultipart builds a multipart body and returns the Content-Type and body.
func formMultipart(t testing.TB, fields url.Values, files map[string][]string) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, vs := range fields {
		for _, v := range vs {
			if err := w.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	for k, contents := range files {
		for _, c := range contents {
			fw, err := w.CreateFormFile(k, k+".txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(fw, c); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return w.FormDataContentType(), buf.String()
}

// TestFormInputTypes 表驱动测试各类型映射。
// TestFormInputTypes is a table-driven test of type mapping.
func TestFormInputTypes(t *testing.T) {
	body := url.Values{
		"id": {"7"}, "s": {"hi"}, "b": {"true"}, "i": {"-3"}, "i8": {"12"}, "u": {"5"}, "u16": {"65535"},
		"f": {"1.5"}, "f32": {"2.5"}, "t": {"2026-01-02T03:04:05Z"}, "lvl": {"high"},
		"p": {"9"}, "ps": {"x"}, "tags": {"a", "b"}, "nums": {"1", "2", "3"}, "lvls": {"low", "high"},
		"NoTag": {"nt"}, "Skip": {"nope"}, "unknown": {"u"},
	}.Encode()
	got, err := FormInput[formAll]().Decode(context.Background(), formReq(formURLEnc, body))
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	checks := []struct {
		name string
		ok   bool
	}{
		{"embedded", got.ID == 7},
		{"string", got.S == "hi"},
		{"bool", got.B},
		{"int", got.I == -3 && got.I8 == 12},
		{"uint", got.U == 5 && got.U16 == 65535},
		{"float", got.F == 1.5 && got.F32 == 2.5},
		{"time", got.T.Equal(want)},
		{"textunmarshaler", got.Lvl == 2},
		{"ptr", got.P != nil && *got.P == 9 && got.PS != nil && *got.PS == "x"},
		{"slice", reflect.DeepEqual(got.Tags, []string{"a", "b"}) && reflect.DeepEqual(got.Nums, []int{1, 2, 3})},
		{"textunmarshaler slice", reflect.DeepEqual(got.Lvls, []formLevel{1, 2})},
		{"untagged", got.NoTag == "nt"},
		{"ignored", got.Skip == ""},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s mismatch: %+v", c.name, got)
		}
	}
}

// TestFormInputPointerTarget 测试 T 为结构体指针，以及缺失/空值保持零值。
// TestFormInputPointerTarget tests a pointer T and zero values for missing/empty input.
func TestFormInputPointerTarget(t *testing.T) {
	got, err := FormInput[*formAll]().Decode(context.Background(), formReq("", "s=a&i=&p=&ps="))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.S != "a" || got.I != 0 || got.P != nil || got.PS == nil || *got.PS != "" {
		t.Errorf("unexpected: %+v", got)
	}
	empty, err := FormInput[formAll]().Decode(context.Background(), formReq("", ""))
	if err != nil || empty.S != "" {
		t.Errorf("empty body: %+v %v", empty, err)
	}
}

// TestFormInputErrors 测试 415 / 400（含字段名）/ strict。
// TestFormInputErrors tests 415, 400 (with field name) and strict mode.
func TestFormInputErrors(t *testing.T) {
	type small struct {
		Age  int       `form:"age"`
		When time.Time `form:"when"`
		Lvl  formLevel `form:"lvl"`
		OK   bool      `form:"ok"`
	}
	tests := []struct {
		name   string
		in     Input[small]
		ct     string
		body   string
		status int
		field  string
		is     error
	}{
		{"415", FormInput[small](), "application/json", `{}`, 415, "", nil},
		{"bad int", FormInput[small](), formURLEnc, "age=abc", 400, `"age"`, ErrInvalidInput},
		{"overflow", FormInput[small](), formURLEnc, "age=99999999999999999999", 400, `"age"`, ErrFormInvalidValue},
		{"bad time", FormInput[small](), formURLEnc, "when=yesterday", 400, `"when"`, ErrInvalidInput},
		{"bad text", FormInput[small](), formURLEnc, "lvl=mid", 400, `"lvl"`, ErrInvalidInput},
		{"bad bool", FormInput[small](), formURLEnc, "ok=maybe", 400, `"ok"`, ErrInvalidInput},
		{"bad escape", FormInput[small](), formURLEnc, "age=%zz", 400, "", nil},
		{"lenient unknown", FormInput[small](), formURLEnc, "x=1&age=2", 0, "", nil},
		{"strict unknown", FormInput[small](WithFormStrict()), formURLEnc, "x=1", 400, `"x"`, ErrFormUnknownField},
		{"strict known", FormInput[small](WithFormStrict()), formURLEnc, "age=2", 0, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.in.Decode(context.Background(), formReq(tt.ct, tt.body))
			if tt.status == 0 {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var sc StatusCoder
			if !errors.As(err, &sc) || sc.HTTPStatus() != tt.status {
				t.Fatalf("err = %v, want status %d", err, tt.status)
			}
			if tt.field != "" && !strings.Contains(err.Error(), tt.field) {
				t.Errorf("error %q lacks field %s", err, tt.field)
			}
			if tt.is != nil && !errors.Is(err, tt.is) {
				t.Errorf("errors.Is(%v, %v) = false", err, tt.is)
			}
		})
	}
}

// TestFormInputUnsupported 测试不支持的目标类型。
// TestFormInputUnsupported tests unsupported target types.
func TestFormInputUnsupported(t *testing.T) {
	_, err := FormInput[int]().Decode(context.Background(), formReq("", "a=1"))
	if !errors.Is(err, ErrFormUnsupportedType) {
		t.Errorf("int target: %v", err)
	}
	type bad struct {
		M map[string]string `form:"m"`
	}
	_, err = FormInput[bad]().Decode(context.Background(), formReq("", "m=1"))
	if !errors.Is(err, ErrFormUnsupportedType) {
		t.Errorf("map field: %v", err)
	}
}

// TestFormInputMultipart 测试 multipart 文本字段与文件上传，含小内存上限落盘。
// TestFormInputMultipart tests multipart fields and uploads, including spill-to-disk.
func TestFormInputMultipart(t *testing.T) {
	for _, mem := range []int64{0, 1} {
		ct, body := formMultipart(t, url.Values{"s": {"hello"}, "nums": {"1", "2"}},
			map[string][]string{"file": {"abc"}, "files": {"x", "yy"}})
		got, err := FormInput[formAll](WithFormMaxMemory(mem)).Decode(context.Background(), formReq(ct, body))
		if err != nil {
			t.Fatal(err)
		}
		if got.S != "hello" || !reflect.DeepEqual(got.Nums, []int{1, 2}) {
			t.Errorf("fields: %+v", got)
		}
		if got.File == nil || got.File.Size != 3 || len(got.Files) != 2 {
			t.Fatalf("files: %+v", got)
		}
		f, err := got.File.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(f)
		_ = f.Close()
		if string(b) != "abc" {
			t.Errorf("file content %q", b)
		}
	}
	// strict：未声明的文件字段 / strict: undeclared file field
	ct, body := formMultipart(t, nil, map[string][]string{"other": {"z"}})
	_, err := FormInput[formAll](WithFormStrict()).Decode(context.Background(), formReq(ct, body))
	if !errors.Is(err, ErrFormUnknownField) {
		t.Errorf("strict file: %v", err)
	}
	// 文件字段收到纯文本值在 strict 下算未知 / a text value for a file field is unknown in strict
	_, err = FormInput[formAll](WithFormStrict()).Decode(context.Background(), formReq(formURLEnc, "file=x"))
	if !errors.Is(err, ErrFormUnknownField) {
		t.Errorf("strict file as text: %v", err)
	}
	// 非法 multipart / malformed multipart
	_, err = FormInput[formAll]().Decode(context.Background(), formReq("multipart/form-data; boundary=zz", "garbage"))
	var sc StatusCoder
	if !errors.As(err, &sc) || sc.HTTPStatus() != 400 {
		t.Errorf("malformed multipart: %v", err)
	}
}

type formE2E struct {
	Name string                `form:"name"`
	Age  int                   `form:"age"`
	Doc  *multipart.FileHeader `form:"doc"`
}

// TestFormInputServer 端到端：通过 Server 注册路由，覆盖 200/400/413/415。
// TestFormInputServer is end to end through a Server, covering 200/400/413/415.
func TestFormInputServer(t *testing.T) {
	h := func(ctx context.Context, req RequestOf[formE2E]) (string, error) {
		f, err := req.Data(ctx)
		if err != nil {
			return "", err
		}
		out := f.Name
		if f.Doc != nil {
			fh, _ := f.Doc.Open()
			b, _ := io.ReadAll(fh)
			_ = fh.Close()
			out += ":" + string(b)
		}
		return out, nil
	}
	s := coreNewServer(t, []ServerOption{WithMaxBodyBytes(2048)},
		Post("/f", h, WithInput(FormInput[formE2E]())),
		Post("/small", h, WithInput(FormInput[formE2E]()), WithBodyLimit(16)),
	)
	mpCT, mpBody := formMultipart(t, url.Values{"name": {"neo"}}, map[string][]string{"doc": {"DATA"}})
	big := "name=" + strings.Repeat("a", 4096)
	tests := []struct {
		name, path, body string
		header           []string
		status           int
		want             string
	}{
		{"urlencoded", "/f", "name=neo&age=3", []string{"Content-Type", formURLEnc}, 200, `"neo"`},
		{"multipart", "/f", mpBody, []string{"Content-Type", mpCT}, 200, `"neo:DATA"`},
		{"bad age", "/f", "age=x", []string{"Content-Type", formURLEnc}, 400, ""},
		{"415", "/f", "{}", []string{"Content-Type", "application/json"}, 415, ""},
		{"413 server limit", "/f", big, []string{"Content-Type", formURLEnc}, 413, ""},
		{"413 route limit", "/small", "name=" + strings.Repeat("a", 64), []string{"Content-Type", formURLEnc}, 413, ""},
		{"413 multipart", "/small", mpBody, []string{"Content-Type", mpCT}, 413, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := coreDo(s, http.MethodPost, tt.path, tt.body, tt.header...)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			if tt.want != "" && rec.Body.String() != tt.want {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.want)
			}
		})
	}
}

// TestFormInputConcurrent 并发解码，配合 -race 验证计划缓存安全。
// TestFormInputConcurrent decodes concurrently to validate the plan cache under -race.
func TestFormInputConcurrent(t *testing.T) {
	in := FormInput[formAll]()
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				got, err := in.Decode(context.Background(), formReq(formURLEnc, "s=x&id=4&tags=a&tags=b"))
				if err != nil || got.S != "x" || got.ID != 4 || len(got.Tags) != 2 {
					t.Errorf("got %+v err %v", got, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestFormPlanCached 测试同一类型只构建一次计划。
// TestFormPlanCached tests that a type's plan is built once.
func TestFormPlanCached(t *testing.T) {
	typ := reflect.TypeOf(formAll{})
	if formPlanFor(typ) != formPlanFor(typ) {
		t.Error("plan not cached")
	}
}

// TestFormOptions 测试选项。
// TestFormOptions tests the options.
func TestFormOptions(t *testing.T) {
	c := formConfig{maxMemory: DefaultFormMaxMemory}
	WithFormMaxMemory(-1)(&c)
	if c.maxMemory != DefaultFormMaxMemory {
		t.Error("non-positive must keep default")
	}
	WithFormMaxMemory(10)(&c)
	WithFormStrict()(&c)
	if c.maxMemory != 10 || !c.strict {
		t.Errorf("%+v", c)
	}
}

// BenchmarkFormInput 基准：urlencoded 解码。
// BenchmarkFormInput benchmarks urlencoded decoding.
func BenchmarkFormInput(b *testing.B) {
	in := FormInput[formAll]()
	body := "s=x&id=4&i=3&b=true&f=1.5&tags=a&tags=b&nums=1&nums=2"
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := in.Decode(ctx, formReq(formURLEnc, body)); err != nil {
			b.Fatal(err)
		}
	}
}
