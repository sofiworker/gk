package app

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

const outputETag = `"ghttp-k6-sample-v1"`

var outputModifiedTime = time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)

type outputJSON struct {
	Message string         `json:"message" required:"true"`
	Tags    []string       `json:"tags"`
	Meta    outputJSONMeta `json:"meta"`
}

type outputJSONRequest struct {
	ghttp.Params
	Missing bool `query:"missing"`
}

type outputJSONMeta struct {
	Source string `json:"source"`
}

type outputXML struct {
	XMLName xml.Name `json:"-" xml:"output"`
	Message string   `xml:"message"`
}

func registerOutput(server *ghttp.Server) {
	sample := loadOutputFixture()

	server.MustMount(ghttp.Handle(ghttp.Get("/output/json"), ghttp.StructInput[outputJSONRequest](), ghttp.JSONOutput[outputJSON](), func(_ context.Context, request outputJSONRequest) (outputJSON, error) {
		if request.Missing {
			return outputJSON{}, ghttp.Err(http.StatusNotFound, http.StatusText(http.StatusNotFound))
		}
		return outputJSON{Message: "hello", Tags: []string{"ghttp", "k6"}, Meta: outputJSONMeta{Source: "typed"}}, nil
	}))
	server.MustMount(ghttp.Handle(ghttp.Get("/output/xml"), ghttp.StructInput[struct{}](), ghttp.XMLOutput[outputXML](), func(context.Context, struct{}) (outputXML, error) {
		return outputXML{Message: "hello"}, nil
	}))
	server.MustMount(ghttp.Handle(ghttp.Get("/output/text"), ghttp.StructInput[struct{}](), ghttp.TextOutput(), func(context.Context, struct{}) (string, error) {
		return "hello text", nil
	}))
	server.MustMount(ghttp.RawOperation(http.MethodGet, "/output/binary", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0, 1, 'g', 'h', 't', 't', 'p', 0xff})
	})))

	server.MustMount(ghttp.Handle(ghttp.Post("/output/created"), ghttp.StructInput[struct{}](), ghttp.WithResponseHeader("Location", "/output/json", ghttp.WithStatus(http.StatusCreated, ghttp.NoContentOutput[struct{}]())), func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, nil
	}))
	server.MustMount(ghttp.Handle(ghttp.Post("/output/accepted"), ghttp.StructInput[struct{}](), ghttp.WithStatus(http.StatusAccepted, ghttp.NoContentOutput[struct{}]()), func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, nil
	}))
	server.MustMount(ghttp.HandleNoOutput(ghttp.Delete("/output/empty"), ghttp.StructInput[struct{}](), func(context.Context, struct{}) error {
		return nil
	}))
	server.MustMount(ghttp.RedirectOperation(http.MethodGet, "/output/redirect", http.StatusTemporaryRedirect, "/output/json"))
	server.MustMount(ghttp.RawOperation(http.MethodGet, "/output/headers", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Vary", "Accept")
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Add("X-Multi", "one")
		w.Header().Add("X-Multi", "two")
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	})))

	server.MustMount(ghttp.RawOperation(http.MethodGet, "/files/sample", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="sample.txt"`)
		http.ServeContent(w, r, "sample.txt", outputModifiedTime, bytes.NewReader(sample))
	})))
	server.MustMount(ghttp.RawOperation(http.MethodGet, "/files/range", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "sample.txt", outputModifiedTime, bytes.NewReader(sample))
	})))
	server.MustMount(ghttp.RawOperation(http.MethodGet, "/files/etag", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", outputETag)
		http.ServeContent(w, r, "sample.txt", outputModifiedTime, bytes.NewReader(sample))
	})))
}

func loadOutputFixture() []byte {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("resolve output fixture source")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "fixtures", "static", "sample.txt"))
	if err != nil {
		panic(err)
	}
	return data
}
