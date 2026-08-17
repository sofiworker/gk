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
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/path/{id}"), ghttp.ValidatedInput(ghttp.StructInput[bindingPathRequest](), func(_ context.Context, in bindingPathRequest) error {
		if in.ID <= 0 {
			return ghttp.Err(http.StatusUnprocessableEntity, "id must be positive")
		}
		return nil
	}), ghttp.JSONOutput[map[string]any](),

		func(_ context.Context, in bindingPathRequest) (map[string]any, error) {
			return map[string]any{"id": in.ID}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/query"), ghttp.ValidatedInput(ghttp.StructInput[bindingQueryRequest](), func(_ context.Context, in bindingQueryRequest) error {
		if in.Name == "" {
			return ghttp.Err(http.StatusUnprocessableEntity, "name is required")
		}
		return nil
	}), ghttp.JSONOutput[map[string]any](),

		func(_ context.Context, in bindingQueryRequest) (map[string]any, error) {
			return map[string]any{"name": in.Name, "count": in.Count, "enabled": in.Enabled, "tags": in.QueryList("tag")}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/header"), ghttp.StructInput[bindingHeaderRequest](), ghttp.JSONOutput[map[string]any](), func(_ context.Context, in bindingHeaderRequest) (map[string]any, error) {
		return map[string]any{"trace_id": in.TraceID, "count": in.Count}, nil
	}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/cookie"), ghttp.StructInput[bindingCookieRequest](), ghttp.JSONOutput[map[string]any](), func(_ context.Context, in bindingCookieRequest) (map[string]any, error) {
		return map[string]any{"session": in.Session}, nil
	}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Post("/binding/mixed/{id}"), ghttp.StructInput[bindingMixedRequest](), ghttp.JSONOutput[map[string]any](), func(_ context.Context, in bindingMixedRequest) (map[string]any, error) {
		return map[string]any{"id": in.ID, "q": in.Query, "trace_id": in.TraceID, "session": in.Session, "name": in.Body.Name}, nil
	}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/time"), ghttp.ValidatedInput(ghttp.StructInput[bindingTimeRequest](), func(_ context.Context, in bindingTimeRequest) error {
		if _, err := time.Parse(time.RFC3339, in.At); err != nil {
			return ghttp.Err(http.StatusUnprocessableEntity, "at must be RFC3339")
		}
		return nil
	}), ghttp.JSONOutput[map[string]any](),

		func(_ context.Context, in bindingTimeRequest) (map[string]any, error) {
			return map[string]any{"at": in.At}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/enum"), ghttp.ValidatedInput(ghttp.StructInput[bindingEnumRequest](), func(_ context.Context, in bindingEnumRequest) error {
		if in.Status != "active" && in.Status != "disabled" {
			return ghttp.Err(http.StatusUnprocessableEntity, "status must be active or disabled")
		}
		return nil
	}), ghttp.JSONOutput[map[string]any](),

		func(_ context.Context, in bindingEnumRequest) (map[string]any, error) {
			return map[string]any{"status": in.Status}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/nested"), ghttp.StructInput[bindingNestedRequest](), ghttp.JSONOutput[map[string]any](), func(_ context.Context, in bindingNestedRequest) (map[string]any, error) {
		return map[string]any{"profile": map[string]any{"name": in.ProfileName, "city": in.ProfileCity}}, nil
	}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Post("/codec/json"), ghttp.StructInput[codecJSONRequest](), ghttp.JSONOutput[map[string]any](), func(_ context.Context, in codecJSONRequest) (map[string]any, error) {
		return map[string]any{"name": in.Body.Name}, nil
	}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Post("/codec/xml"), ghttp.StructInput[codecXMLRequest](ghttp.MIMEXML), ghttp.JSONOutput[map[string]any](), func(_ context.Context, in codecXMLRequest) (map[string]any, error) {
		return map[string]any{"name": in.Body.Name}, nil
	}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Post("/codec/form"), ghttp.StructInput[codecFormRequest](ghttp.MIMEPOSTForm), ghttp.JSONOutput[map[string]any](), func(_ context.Context, in codecFormRequest) (map[string]any, error) {
		return map[string]any{"name": in.Body.Name, "note": in.Body.Note}, nil
	}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Post("/codec/multipart"), ghttp.StructInput[codecMultipartRequest](ghttp.MIMEMultipartPOSTForm), ghttp.JSONOutput[map[string]any](), func(_ context.Context, in codecMultipartRequest) (map[string]any, error) {
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
	}).WithProblemDetails())
	codecManager := ghttp.NewCodecManager()
	server.MustMount(ghttp.HandleHTTP(ghttp.Post("/codec/negotiate"), ghttp.StructInput[codecJSONRequest](ghttp.MIMEJSON), func(w http.ResponseWriter, r *http.Request, in codecJSONRequest) error {
		response := codecNegotiationResponse{Name: in.Body.Name}
		contentType, codec, ok := codecManager.Select(r.Header.Get("Accept"), []string{ghttp.MIMEJSON, ghttp.MIMEXML})
		if !ok {
			return ghttp.Err(http.StatusNotAcceptable, http.StatusText(http.StatusNotAcceptable))
		}
		w.Header().Set("Content-Type", contentType)
		return codec.Marshal(w, response)
	}).WithProblemDetails())
	limit := ghttp.Handle(ghttp.Post("/codec/body/limited"), ghttp.StructInput[codecBodyRequest](), ghttp.JSONOutput[map[string]any](), func(_ context.Context, in codecBodyRequest) (map[string]any, error) {
		return map[string]any{"size": len(in.Body.Data)}, nil
	}).WithProblemDetails()
	if cfg.MaxBodyBytes > 0 {
		limit = limit.WithMaxBodyBytes(cfg.MaxBodyBytes)
	}
	server.MustMount(limit)
}
