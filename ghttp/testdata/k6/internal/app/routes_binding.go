package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sofiworker/gk/ghttp"
)

func registerBinding(server *ghttp.Server, cfg Config) {
	mustRaw(server.RawHandle(http.MethodGet, "/binding/path/{id}", func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		id, err := strconv.ParseInt(req.Params.Get("id"), 10, 64)
		if err != nil {
			writeProblemError(resp, http.StatusBadRequest, "path id must be an integer")
			return nil
		}
		if id <= 0 {
			writeProblemError(resp, http.StatusUnprocessableEntity, "id must be positive")
			return nil
		}
		writeJSON(resp, map[string]any{"id": id})
		return nil
	}))

	mustRaw(server.RawHandle(http.MethodGet, "/binding/query", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var count int64
		if raw := q.Get("count"); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				writeProblemError(w, http.StatusBadRequest, "count must be an integer")
				return
			}
			count = parsed
		}
		var enabled bool
		if raw := q.Get("enabled"); raw != "" {
			parsed, err := strconv.ParseBool(raw)
			if err != nil {
				writeProblemError(w, http.StatusBadRequest, "enabled must be a boolean")
				return
			}
			enabled = parsed
		}
		name := q.Get("name")
		if name == "" {
			writeProblemError(w, http.StatusUnprocessableEntity, "name is required")
			return
		}
		writeJSON(w, map[string]any{"name": name, "count": count, "enabled": enabled, "tags": q["tag"]})
	}))))

	mustRaw(server.RawHandle(http.MethodGet, "/binding/header", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count, _ := strconv.ParseInt(r.Header.Get("X-Count"), 10, 64)
		writeJSON(w, map[string]any{"trace_id": r.Header.Get("X-Trace-ID"), "count": count})
	}))))

	mustRaw(server.RawHandle(http.MethodGet, "/binding/cookie", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := ""
		if cookie, err := r.Cookie("session"); err == nil {
			session = cookie.Value
		}
		writeJSON(w, map[string]any{"session": session})
	}))))

	mustRaw(server.RawHandle(http.MethodPost, "/binding/mixed/{id}", func(_ context.Context, req *ghttp.Request, resp *ghttp.Response) error {
		r := req.Request
		id, err := strconv.ParseInt(req.Params.Get("id"), 10, 64)
		if err != nil {
			writeProblemError(resp, http.StatusBadRequest, "path id must be an integer")
			return nil
		}
		session := ""
		if cookie, err := r.Cookie("session"); err == nil {
			session = cookie.Value
		}
		var body struct {
			Name string `json:"name"`
		}
		if !decodeJSON(resp, r, &body) {
			return nil
		}
		writeJSON(resp, map[string]any{
			"id":       id,
			"q":        r.URL.Query().Get("q"),
			"trace_id": r.Header.Get("X-Trace-ID"),
			"session":  session,
			"name":     body.Name,
		})
		return nil
	}))

	mustRaw(server.RawHandle(http.MethodGet, "/binding/time", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		at := r.URL.Query().Get("at")
		if _, err := time.Parse(time.RFC3339, at); err != nil {
			writeProblemError(w, http.StatusUnprocessableEntity, "at must be RFC3339")
			return
		}
		writeJSON(w, map[string]any{"at": at})
	}))))

	mustRaw(server.RawHandle(http.MethodGet, "/binding/enum", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		if status != "active" && status != "disabled" {
			writeProblemError(w, http.StatusUnprocessableEntity, "status must be active or disabled")
			return
		}
		writeJSON(w, map[string]any{"status": status})
	}))))

	mustRaw(server.RawHandle(http.MethodGet, "/binding/nested", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		writeJSON(w, map[string]any{"profile": map[string]any{
			"name": q.Get("profile_name"),
			"city": q.Get("profile_city"),
		}})
	}))))

	mustRaw(server.RawHandle(http.MethodPost, "/codec/json", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := mediaType(r.Header.Get("Content-Type")); ct != "" && ct != "application/json" {
			writeProblemError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
			return
		}
		var in codecJSONRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeProblemError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		writeJSON(w, map[string]any{"name": in.Name})
	}))))

	mustRaw(server.RawHandle(http.MethodPost, "/codec/xml", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := mediaType(r.Header.Get("Content-Type")); ct != "" && ct != "application/xml" && ct != "text/xml" {
			writeProblemError(w, http.StatusUnsupportedMediaType, "content type must be application/xml")
			return
		}
		var payload struct {
			Name string `xml:"name"`
		}
		if err := xml.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeProblemError(w, http.StatusBadRequest, "invalid XML")
			return
		}
		writeJSON(w, map[string]any{"name": payload.Name})
	}))))

	mustRaw(server.RawHandle(http.MethodPost, "/codec/form", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			writeProblemError(w, http.StatusBadRequest, "invalid form")
			return
		}
		writeJSON(w, map[string]any{"name": r.FormValue("name"), "note": r.FormValue("note")})
	}))))

	mustRaw(server.RawHandle(http.MethodPost, "/codec/multipart", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeProblemError(w, http.StatusBadRequest, "invalid multipart")
			return
		}
		note := r.FormValue("note")
		var files []*multipart.FileHeader
		if r.MultipartForm != nil {
			files = r.MultipartForm.File["file"]
		}
		if len(files) != 1 {
			writeProblemError(w, http.StatusBadRequest, "exactly one file required")
			return
		}
		file := files[0]
		if file.Size > 64*1024 {
			writeProblemError(w, http.StatusRequestEntityTooLarge, "file too large")
			return
		}
		opened, err := file.Open()
		if err != nil {
			writeProblemError(w, http.StatusBadRequest, "cannot open file")
			return
		}
		defer opened.Close()
		data, err := io.ReadAll(opened)
		if err != nil {
			writeProblemError(w, http.StatusBadRequest, "cannot read file")
			return
		}
		sum := sha256.Sum256(data)
		writeJSON(w, map[string]any{
			"note":     note,
			"filename": file.Filename,
			"size":     len(data),
			"hash":     hex.EncodeToString(sum[:]),
		})
	}))))

	mustRaw(server.RawHandle(http.MethodPost, "/codec/negotiate", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in codecJSONRequest
		if !decodeJSON(w, r, &in) {
			return
		}
		accept := r.Header.Get("Accept")
		ct, ok := negotiateAccept(accept)
		if !ok {
			writeProblemError(w, http.StatusNotAcceptable, "no acceptable content type")
			return
		}
		w.Header().Set("Content-Type", ct)
		switch ct {
		case "application/xml":
			_ = xml.NewEncoder(w).Encode(codecNegotiationResponse{Name: in.Name})
		default:
			writeJSON(w, map[string]any{"name": in.Name})
		}
	}))))

	mustRaw(server.RawHandle(http.MethodPost, "/codec/body/limited", rawAdapter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := cfg.MaxBodyBytes
		if limit <= 0 {
			limit = 4 << 20 // 默认 4 MB / default 4 MB
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		var in codecBodyRequest
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeProblemError(w, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		writeJSON(w, map[string]any{"size": len(in.Data)})
	}))))
}

// negotiateAccept 解析 Accept 头部,按 q 值选择最佳支持的媒体类型(json/xml)。
// negotiateAccept parses the Accept header, selecting the best supported media
// type (json/xml) by q-value.
func negotiateAccept(accept string) (string, bool) {
	if accept == "" || accept == "*/*" {
		return "application/json", true
	}
	best, bestQ := "", -1.0
	for _, part := range strings.Split(accept, ",") {
		fields := strings.Split(part, ";")
		mt := strings.TrimSpace(fields[0])
		q := 1.0
		for _, p := range fields[1:] {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "q=") {
				q, _ = strconv.ParseFloat(strings.TrimPrefix(p, "q="), 64)
			}
		}
		if q > bestQ {
			bestQ = q
			best = mt
		}
	}
	if best == "application/json" || best == "application/xml" {
		return best, true
	}
	return "", false
}

// mediaType 提取 Content-Type 的媒体类型部分(去掉参数)。
// mediaType extracts the media type portion of a Content-Type (stripping params).
func mediaType(contentType string) string {
	if idx := strings.IndexByte(contentType, ';'); idx >= 0 {
		return strings.TrimSpace(contentType[:idx])
	}
	return strings.TrimSpace(contentType)
}
