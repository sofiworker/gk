package gserver

import (
	"fmt"
	"gk/ghttp"
	"net"
	"os"
)

// TrustProxyConfig is a struct for configuring trusted proxies if Config.TrustProxy is true.
type TrustProxyConfig struct {
	ips map[string]struct{}

	// Proxies is a list of trusted proxy IP addresses or CIDR ranges.
	//
	// Default: []string
	Proxies []string `json:"proxies"`

	ranges []*net.IPNet

	// LinkLocal enables trusting all link-local IP ranges (e.g., 169.254.0.0/16, fe80::/10).
	//
	// Default: false
	LinkLocal bool `json:"link_local"`

	// Loopback enables trusting all loopback IP ranges (e.g., 127.0.0.0/8, ::1/128).
	//
	// Default: false
	Loopback bool `json:"loopback"`

	// Private enables trusting all private IP ranges (e.g., 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7).
	//
	// Default: false
	Private bool `json:"private"`
}

type Server struct {
	codecManager *ghttp.CodecManager
}

func NewServer() *Server {
	return &Server{
		codecManager: ghttp.NewCodecManager(),
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
func (s *Server) SetDefaultCodec(codec ghttp.Codec) {
	s.codecManager.SetDefaultCodec(codec)
}

// RegisterCodec 注册编解码器
func (s *Server) RegisterCodec(codec ghttp.Codec) {
	s.codecManager.RegisterCodec(codec)
}

// GetCodec 获取编解码器
func (s *Server) GetCodec(contentType string) ghttp.Codec {
	return s.codecManager.GetCodec(contentType)
}

// ForceCodec 强制使用指定编解码器（用于特殊情况）
func (s *Server) ForceCodec(codec ghttp.Codec) ghttp.Codec {
	return codec
}
