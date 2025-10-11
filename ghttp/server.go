package ghttp

import (
	"fmt"
	"net"
	"os"
)

type Server struct {
	codecManager *CodecManager
}

func NewServer() *Server {
	return &Server{
		codecManager: NewCodecManager(),
	}
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

// SetDefaultCodec 设置默认编解码器
func (s *Server) SetDefaultCodec(codec Codec) {
	s.codecManager.SetDefaultCodec(codec)
}

// RegisterCodec 注册编解码器
func (s *Server) RegisterCodec(codec Codec) {
	s.codecManager.RegisterCodec(codec)
}

// GetCodec 获取编解码器
func (s *Server) GetCodec(contentType string) Codec {
	return s.codecManager.GetCodec(contentType)
}

// ForceCodec 强制使用指定编解码器（用于特殊情况）
func (s *Server) ForceCodec(codec Codec) Codec {
	return codec
}
