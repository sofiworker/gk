package ghttp

import (
	"fmt"
	"net"
	"os"
)

type Server struct {
}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) ListenAndServe(addr string) error {

	return nil
}

func (s *Server) ListenAndServeUNIX(addr string, mode os.FileMode) error {
	if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return err
	}
	if err = os.Chmod(addr, mode); err != nil {
		return fmt.Errorf("cannot chmod %#o for %q: %w", mode, addr, err)
	}
	return s.Serve(ln)
}

func (s *Server) Serve(ln net.Listener) error {
	return nil
}

func (s *Server) Router() *Router {
	return &Router{}
}
