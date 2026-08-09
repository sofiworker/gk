package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

func registerBinding(server *ghttp.Server, cfg Config) {
	ghttp.Route[bindingPathRequest, map[string]any](server).GET("/binding/path/{id}").ProblemDetails().Validate(func(in bindingPathRequest) error {
		if in.ID <= 0 {
			return ghttp.Err(http.StatusUnprocessableEntity, "id must be positive")
		}
		return nil
	}).To(func(_ context.Context, in bindingPathRequest) (map[string]any, error) {
		return map[string]any{"id": in.ID}, nil
	})
	ghttp.Route[bindingQueryRequest, map[string]any](server).GET("/binding/query").ProblemDetails().Validate(func(in bindingQueryRequest) error {
		if in.Name == "" {
			return ghttp.Err(http.StatusUnprocessableEntity, "name is required")
		}
		return nil
	}).To(func(_ context.Context, in bindingQueryRequest) (map[string]any, error) {
		return map[string]any{"name": in.Name, "count": in.Count, "enabled": in.Enabled, "tags": in.QueryList("tag")}, nil
	})
	ghttp.Route[bindingHeaderRequest, map[string]any](server).GET("/binding/header").ProblemDetails().To(func(_ context.Context, in bindingHeaderRequest) (map[string]any, error) {
		return map[string]any{"trace_id": in.TraceID, "count": in.Count}, nil
	})
	ghttp.Route[bindingCookieRequest, map[string]any](server).GET("/binding/cookie").ProblemDetails().To(func(_ context.Context, in bindingCookieRequest) (map[string]any, error) {
		return map[string]any{"session": in.Session}, nil
	})
	ghttp.Route[bindingMixedRequest, map[string]any](server).POST("/binding/mixed/{id}").ProblemDetails().To(func(_ context.Context, in bindingMixedRequest) (map[string]any, error) {
		return map[string]any{"id": in.ID, "q": in.Query, "trace_id": in.TraceID, "session": in.Session, "name": in.Body.Name}, nil
	})
	ghttp.Route[bindingTimeRequest, map[string]any](server).GET("/binding/time").ProblemDetails().Validate(func(in bindingTimeRequest) error {
		if _, err := time.Parse(time.RFC3339, in.At); err != nil {
			return ghttp.Err(http.StatusUnprocessableEntity, "at must be RFC3339")
		}
		return nil
	}).To(func(_ context.Context, in bindingTimeRequest) (map[string]any, error) {
		return map[string]any{"at": in.At}, nil
	})
	ghttp.Route[bindingEnumRequest, map[string]any](server).GET("/binding/enum").ProblemDetails().Validate(func(in bindingEnumRequest) error {
		if in.Status != "active" && in.Status != "disabled" {
			return ghttp.Err(http.StatusUnprocessableEntity, "status must be active or disabled")
		}
		return nil
	}).To(func(_ context.Context, in bindingEnumRequest) (map[string]any, error) {
		return map[string]any{"status": in.Status}, nil
	})
	ghttp.Route[bindingNestedRequest, map[string]any](server).GET("/binding/nested").ProblemDetails().To(func(_ context.Context, in bindingNestedRequest) (map[string]any, error) {
		return map[string]any{"profile": map[string]any{"name": in.ProfileName, "city": in.ProfileCity}}, nil
	})
	ghttp.Route[codecJSONRequest, map[string]any](server).POST("/codec/json").ProblemDetails().To(func(_ context.Context, in codecJSONRequest) (map[string]any, error) {
		return map[string]any{"name": in.Body.Name}, nil
	})
	ghttp.Route[codecXMLRequest, map[string]any](server).POST("/codec/xml").Consumes(ghttp.MIMEXML).ProblemDetails().To(func(_ context.Context, in codecXMLRequest) (map[string]any, error) {
		return map[string]any{"name": in.Body.Name}, nil
	})
	ghttp.Route[codecFormRequest, map[string]any](server).POST("/codec/form").Consumes(ghttp.MIMEPOSTForm).ProblemDetails().To(func(_ context.Context, in codecFormRequest) (map[string]any, error) {
		return map[string]any{"name": in.Body.Name, "note": in.Body.Note}, nil
	})
	ghttp.Route[codecMultipartRequest, map[string]any](server).POST("/codec/multipart").Consumes(ghttp.MIMEMultipartPOSTForm).ProblemDetails().To(func(_ context.Context, in codecMultipartRequest) (map[string]any, error) {
		if len(in.Body.Files) != 1 {
			return nil, ghttp.BadRequest("file is required")
		}
		file := in.Body.Files[0]
		if file.Size > 64*1024 {
			return nil, ghttp.Err(http.StatusRequestEntityTooLarge, "file is too large")
		}
		data, err := file.Bytes()
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		return map[string]any{"note": in.Body.Note, "filename": file.Filename, "size": len(data), "hash": hex.EncodeToString(sum[:])}, nil
	})
	neg := ghttp.Route[codecJSONRequest, codecNegotiationResponse](server).POST("/codec/negotiate").Consumes(ghttp.MIMEJSON).ProblemDetails()
	codecManager := ghttp.NewCodecManager()
	neg.ToHTTPFunc(func(w http.ResponseWriter, r *http.Request, in codecJSONRequest) error {
		response := codecNegotiationResponse{Name: in.Body.Name}
		contentType, codec, ok := codecManager.Select(r.Header.Get("Accept"), []string{ghttp.MIMEJSON, ghttp.MIMEXML})
		if !ok {
			return ghttp.Err(http.StatusNotAcceptable, http.StatusText(http.StatusNotAcceptable))
		}
		w.Header().Set("Content-Type", contentType)
		return codec.Marshal(w, response)
	})
	limit := ghttp.Route[codecBodyRequest, map[string]any](server).POST("/codec/body/limited").ProblemDetails()
	if cfg.MaxBodyBytes > 0 {
		limit.MaxBodyBytes(cfg.MaxBodyBytes)
	}
	limit.To(func(_ context.Context, in codecBodyRequest) (map[string]any, error) {
		return map[string]any{"size": len(in.Body.Data)}, nil
	})
}
