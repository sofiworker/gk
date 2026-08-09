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

	ghttp.Route[outputJSONRequest, outputJSON](server).GET("/output/json").Produces(ghttp.MIMEJSON).To(func(_ context.Context, request outputJSONRequest) (outputJSON, error) {
		if request.Missing {
			return outputJSON{}, ghttp.Err(http.StatusNotFound, http.StatusText(http.StatusNotFound))
		}
		return outputJSON{Message: "hello", Tags: []string{"ghttp", "k6"}, Meta: outputJSONMeta{Source: "typed"}}, nil
	})
	ghttp.Route[struct{}, outputXML](server).GET("/output/xml").Produces(ghttp.MIMEXML).To(func(context.Context, struct{}) (outputXML, error) {
		return outputXML{Message: "hello"}, nil
	})
	ghttp.Route[struct{}, string](server).GET("/output/text").Produces(ghttp.MIMEPlain).To(func(context.Context, struct{}) (string, error) {
		return "hello text", nil
	})
	ghttp.Route[struct{}, struct{}](server).GET("/output/binary").ToRaw(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0, 1, 'g', 'h', 't', 't', 'p', 0xff})
	})

	ghttp.Route[struct{}, struct{}](server).POST("/output/created").Status(http.StatusCreated).ResponseHeader("Location", "/output/json").ToNoOutput(func(context.Context, struct{}) error {
		return nil
	})
	ghttp.Route[struct{}, struct{}](server).POST("/output/accepted").Status(http.StatusAccepted).ToNoOutput(func(context.Context, struct{}) error {
		return nil
	})
	ghttp.Route[struct{}, struct{}](server).DELETE("/output/empty").ToNoOutput(func(context.Context, struct{}) error {
		return nil
	})
	ghttp.Route[struct{}, struct{}](server).GET("/output/redirect").ToRedirect(http.StatusTemporaryRedirect, "/output/json")
	ghttp.Route[struct{}, struct{}](server).GET("/output/headers").ToRaw(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Vary", "Accept")
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Add("X-Multi", "one")
		w.Header().Add("X-Multi", "two")
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	})

	ghttp.Route[struct{}, struct{}](server).GET("/files/sample").ToRaw(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="sample.txt"`)
		http.ServeContent(w, r, "sample.txt", outputModifiedTime, bytes.NewReader(sample))
	})
	ghttp.Route[struct{}, struct{}](server).GET("/files/range").ToRaw(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "sample.txt", outputModifiedTime, bytes.NewReader(sample))
	})
	ghttp.Route[struct{}, struct{}](server).GET("/files/etag").ToRaw(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", outputETag)
		http.ServeContent(w, r, "sample.txt", outputModifiedTime, bytes.NewReader(sample))
	})
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
