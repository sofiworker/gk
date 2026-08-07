package ghttp

import "net/http"

// ErrorWriter writes an HTTP error response. It returns true when the
// response has been handled; returning false lets the framework fall through
// to the next writer or its built-in error writer.
type ErrorWriter func(w http.ResponseWriter, r *http.Request, status int, err error) bool

// ChainErrorWriters composes writers outermost-first. Each writer runs until
// one handles the response; if none handles it, the composed writer returns
// false and the framework falls back to its default error writer.
func ChainErrorWriters(writers ...ErrorWriter) ErrorWriter {
	return func(w http.ResponseWriter, r *http.Request, status int, err error) bool {
		for _, writer := range writers {
			if writer == nil {
				continue
			}
			if writer(w, r, status, err) {
				return true
			}
		}
		return false
	}
}

func problemErrorWriter(s *Server) ErrorWriter {
	return func(w http.ResponseWriter, r *http.Request, status int, err error) bool {
		writeProblemDetails(w, r, s, status, err)
		return true
	}
}
