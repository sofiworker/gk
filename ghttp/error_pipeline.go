package ghttp

import (
	"encoding/json"
	"net/http"
)

func (s *Server) RespondError(w http.ResponseWriter, r *http.Request, err error) {
	if s == nil {
		writeMinimalInternalError(w, r)
		return
	}
	s.respondError(w, r, http.StatusInternalServerError, err)
}

func RespondError(w http.ResponseWriter, r *http.Request, err error) {
	if server := serverFromRequest(r); server != nil {
		server.RespondError(w, r, err)
		return
	}
	writeMinimalInternalError(w, r)
}

func (s *Server) respondError(w http.ResponseWriter, r *http.Request, defaultStatus int, err error) {
	if responseErrorWriteBlocked(r) || !beginResponseErrorHandler(r) {
		s.observeError(r, safeInternalError(err), nil, true)
		return
	}
	normalized := s.normalizeError(r, err)
	if normalized.Document.MessageID == internalServerErrorID && defaultStatus != http.StatusInternalServerError && validHTTPErrorStatus(defaultStatus) {
		normalized.Document.Status = defaultStatus
		normalized.Document.MessageID = frameworkMessageID(defaultStatus)
	}
	if normalized.SuppressResponse {
		s.observeError(r, normalized, nil, false)
		return
	}
	rendererErr := renderErrorSafely(s.errorRenderer, w, r, normalized.Document)
	if rendererErr != nil {
		writeMinimalInternalError(w, r)
	}
	s.observeError(r, normalized, rendererErr, true)
}

func (s *Server) normalizeError(r *http.Request, err error) (normalized NormalizedError) {
	ctx := r.Context()
	defer func() {
		if recover() != nil {
			normalized = safeInternalError(err)
		}
	}()
	return finalizeNormalizedError(s.errorNormalizer.NormalizeError(ctx, err))
}

func renderErrorSafely(renderer ErrorRenderer, w http.ResponseWriter, r *http.Request, document ErrorDocument) (renderErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			renderErr = ErrRendererNotConfigured
		}
	}()
	if renderer == nil {
		return ErrRendererNotConfigured
	}
	return renderer.RenderError(w, r, cloneErrorDocument(document))
}

func (s *Server) observeError(r *http.Request, normalized NormalizedError, rendererErr error, committed bool) {
	if s == nil || r == nil {
		return
	}
	observation := ErrorObservation{
		Status: normalized.Document.Status, MessageID: normalized.Document.MessageID,
		Kind: normalized.Kind, Op: normalized.Op, Cause: normalized.Cause,
		Meta: cloneDiagnosticMap(normalized.Meta), RendererError: rendererErr, Committed: committed,
	}
	for _, observer := range s.errorObservers {
		func() {
			defer func() { _ = recover() }()
			if observer != nil {
				observer.ObserveError(r.Context(), observation)
			}
		}()
	}
}

func frameworkMessageID(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "request.invalid_body"
	case http.StatusNotFound:
		return "http.route_not_found"
	case http.StatusMethodNotAllowed:
		return "http.method_not_allowed"
	case http.StatusRequestEntityTooLarge:
		return "request.body_too_large"
	case http.StatusUnsupportedMediaType:
		return "request.unsupported_media_type"
	case http.StatusUnprocessableEntity:
		return "request.validation_failed"
	default:
		return internalServerErrorID
	}
}

func writeMinimalInternalError(w http.ResponseWriter, r *http.Request) {
	if w == nil {
		return
	}
	w.Header().Set("Content-Type", MIMEJSON)
	w.WriteHeader(http.StatusInternalServerError)
	if r != nil && r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(ErrorDocument{Status: 500, MessageID: internalServerErrorID, Args: map[string]any{}})
}
