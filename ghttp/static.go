package ghttp

import (
	"net/http"
	"strings"
)

func (s *Server) Static(relativePath, root string) {
	if root == "" {
		panic("static root cannot be empty")
	}
	s.StaticFS(relativePath, http.Dir(root))
}

func (s *Server) StaticFS(relativePath string, fs http.FileSystem) {
	prefix := strings.TrimRight(relativePath, "/")
	handler := http.StripPrefix(prefix, http.FileServer(fs))

	absolutePath := JoinPaths(relativePath, "/*path")
	_ = s.router.Register(http.MethodGet, absolutePath, handler)
	_ = s.router.Register(http.MethodHead, absolutePath, handler)
}

func (s *Server) StaticFile(relativePath, filepath string) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath)
	})
	_ = s.router.Register(http.MethodGet, relativePath, handler)
	_ = s.router.Register(http.MethodHead, relativePath, handler)
}

func (s *Server) createStaticHandler(fs http.FileSystem) http.Handler {
	fileServer := http.FileServer(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "..") {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
