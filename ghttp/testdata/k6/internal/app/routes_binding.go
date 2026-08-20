package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"strconv"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

func registerBinding(server *ghttp.Server, cfg Config) {
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/path/{id}"),
		ghttp.ValidatedInput(ghttp.PathInt64("id"), func(_ context.Context, id int64) error {
			if id <= 0 {
				return ghttp.Err(http.StatusUnprocessableEntity, "id must be positive")
			}
			return nil
		}), ghttp.JSONOutput[map[string]any](),
		func(_ context.Context, id int64) (map[string]any, error) {
			return map[string]any{"id": id}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/query"),
		ghttp.ValidatedInput(ghttp.InputFunc(func(view ghttp.RequestView) (bindingQueryRequest, error) {
			q := view.Query()
			count, _ := strconv.ParseInt(q.Get("count"), 10, 64)
			enabled, _ := strconv.ParseBool(q.Get("enabled"))
			return bindingQueryRequest{
				Name:    q.Get("name"),
				Count:   count,
				Enabled: enabled,
				Tags:    q["tag"],
			}, nil
		}), func(_ context.Context, in bindingQueryRequest) error {
			if in.Name == "" {
				return ghttp.Err(http.StatusUnprocessableEntity, "name is required")
			}
			return nil
		}), ghttp.JSONOutput[map[string]any](),
		func(_ context.Context, in bindingQueryRequest) (map[string]any, error) {
			return map[string]any{"name": in.Name, "count": in.Count, "enabled": in.Enabled, "tags": in.Tags}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/header"),
		ghttp.InputFunc(func(view ghttp.RequestView) (bindingHeaderRequest, error) {
			count, _ := strconv.ParseInt(view.Header().Get("X-Count"), 10, 64)
			return bindingHeaderRequest{TraceID: view.Header().Get("X-Trace-ID"), Count: count}, nil
		}), ghttp.JSONOutput[map[string]any](),
		func(_ context.Context, in bindingHeaderRequest) (map[string]any, error) {
			return map[string]any{"trace_id": in.TraceID, "count": in.Count}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/cookie"), ghttp.CookieString("session"), ghttp.JSONOutput[map[string]any](), func(_ context.Context, session string) (map[string]any, error) {
		return map[string]any{"session": session}, nil
	}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Post("/binding/mixed/{id}"),
		ghttp.MapInputs5(
			ghttp.PathInt64("id"),
			ghttp.QueryString("q"),
			ghttp.HeaderString("X-Trace-ID"),
			ghttp.CookieString("session"),
			ghttp.JSONBody[struct {
				Name string `json:"name"`
			}](),
			func(id int64, q, traceID, session string, body struct {
				Name string `json:"name"`
			}) bindingMixedRequest {
				return bindingMixedRequest{ID: id, Query: q, TraceID: traceID, Session: session, Name: body.Name}
			},
		), ghttp.JSONOutput[map[string]any](),
		func(_ context.Context, in bindingMixedRequest) (map[string]any, error) {
			return map[string]any{"id": in.ID, "q": in.Query, "trace_id": in.TraceID, "session": in.Session, "name": in.Name}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/time"),
		ghttp.ValidatedInput(ghttp.QueryString("at"), func(_ context.Context, at string) error {
			if _, err := time.Parse(time.RFC3339, at); err != nil {
				return ghttp.Err(http.StatusUnprocessableEntity, "at must be RFC3339")
			}
			return nil
		}), ghttp.JSONOutput[map[string]any](),
		func(_ context.Context, at string) (map[string]any, error) {
			return map[string]any{"at": at}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/enum"),
		ghttp.ValidatedInput(ghttp.QueryString("status"), func(_ context.Context, status string) error {
			if status != "active" && status != "disabled" {
				return ghttp.Err(http.StatusUnprocessableEntity, "status must be active or disabled")
			}
			return nil
		}), ghttp.JSONOutput[map[string]any](),
		func(_ context.Context, status string) (map[string]any, error) {
			return map[string]any{"status": status}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Get("/binding/nested"),
		ghttp.MapInputs(ghttp.QueryString("profile_name"), ghttp.QueryString("profile_city"),
			func(name, city string) bindingNestedRequest {
				return bindingNestedRequest{ProfileName: name, ProfileCity: city}
			}), ghttp.JSONOutput[map[string]any](),
		func(_ context.Context, in bindingNestedRequest) (map[string]any, error) {
			return map[string]any{"profile": map[string]any{"name": in.ProfileName, "city": in.ProfileCity}}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Post("/codec/json"), ghttp.JSONBody[codecJSONRequest](), ghttp.JSONOutput[map[string]any](), func(_ context.Context, in codecJSONRequest) (map[string]any, error) {
		return map[string]any{"name": in.Name}, nil
	}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Post("/codec/xml"),
		ghttp.InputFunc(func(view ghttp.RequestView) (codecXMLRequest, error) {
			var payload struct {
				Name string `xml:"name"`
			}
			if err := xml.NewDecoder(view.HTTPRequest().Body).Decode(&payload); err != nil {
				return codecXMLRequest{}, err
			}
			return codecXMLRequest{Name: payload.Name}, nil
		}), ghttp.JSONOutput[map[string]any](),
		func(_ context.Context, in codecXMLRequest) (map[string]any, error) {
			return map[string]any{"name": in.Name}, nil
		}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Post("/codec/form"), ghttp.FormBody(), ghttp.JSONOutput[map[string]any](), func(_ context.Context, values map[string][]string) (map[string]any, error) {
		name, note := "", ""
		if got, ok := values["name"]; ok && len(got) > 0 {
			name = got[0]
		}
		if got, ok := values["note"]; ok && len(got) > 0 {
			note = got[0]
		}
		return map[string]any{"name": name, "note": note}, nil
	}).WithProblemDetails())
	server.MustMount(ghttp.Handle(ghttp.Post("/codec/multipart"),
		ghttp.InputFunc(func(view ghttp.RequestView) (codecMultipartRequest, error) {
			r := view.HTTPRequest()
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				return codecMultipartRequest{}, err
			}
			req := codecMultipartRequest{Note: r.FormValue("note")}
			if r.MultipartForm != nil {
				for _, fh := range r.MultipartForm.File["file"] {
					req.Files = append(req.Files, &ghttp.FileHeader{FileHeader: fh})
				}
			}
			return req, nil
		}), ghttp.JSONOutput[map[string]any](),
		func(_ context.Context, in codecMultipartRequest) (map[string]any, error) {
			if len(in.Files) != 1 {
				return nil, ghttp.BadRequest("file is required")
			}
			file := in.Files[0]
			if file.Size > 64*1024 {
				return nil, ghttp.Err(http.StatusRequestEntityTooLarge, "file is too large")
			}
			data, err := file.Bytes()
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256(data)
			return map[string]any{"note": in.Note, "filename": file.Filename, "size": len(data), "hash": hex.EncodeToString(sum[:])}, nil
		}).WithProblemDetails())
	codecManager := ghttp.NewCodecManager()
	server.MustMount(ghttp.HandleHTTP(ghttp.Post("/codec/negotiate"),
		ghttp.InputFunc(func(view ghttp.RequestView) (codecJSONRequest, error) {
			var payload codecJSONRequest
			if err := json.NewDecoder(view.HTTPRequest().Body).Decode(&payload); err != nil {
				return codecJSONRequest{}, err
			}
			return payload, nil
		}),
		func(w http.ResponseWriter, r *http.Request, in codecJSONRequest) error {
			response := codecNegotiationResponse{Name: in.Name}
			contentType, codec, ok := codecManager.Select(r.Header.Get("Accept"), []string{ghttp.MIMEJSON, ghttp.MIMEXML})
			if !ok {
				return ghttp.Err(http.StatusNotAcceptable, http.StatusText(http.StatusNotAcceptable))
			}
			w.Header().Set("Content-Type", contentType)
			return codec.Marshal(w, response)
		}).WithProblemDetails())
	limit := ghttp.Handle(ghttp.Post("/codec/body/limited"), ghttp.JSONBody[codecBodyRequest](), ghttp.JSONOutput[map[string]any](), func(_ context.Context, in codecBodyRequest) (map[string]any, error) {
		return map[string]any{"size": len(in.Data)}, nil
	}).WithProblemDetails()
	if cfg.MaxBodyBytes > 0 {
		limit = limit.WithMaxBodyBytes(cfg.MaxBodyBytes)
	}
	server.MustMount(limit)
}
