package ghttp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// listenLocal 在回环上取一个临时端口的监听器。
// listenLocal opens a listener on an ephemeral loopback port.
func listenLocal(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return ln
}

// TestServer_EndToEnd_HTTP 端到端验证：真实监听→真实客户端请求→优雅关闭。
func TestServer_EndToEnd_HTTP(t *testing.T) {
	m := New()
	if err := m.RawHandle(http.MethodGet, "/ping", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.WriteString("pong")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	ln := listenLocal(t)
	srv := NewServer(m)

	var serveErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		serveErr = srv.Serve(ln)
	}()

	url := "http://" + ln.Addr().String() + "/ping"
	body, code := getWithRetry(t, url, nil)
	if code != http.StatusOK {
		t.Errorf("GET /ping status=%d, want 200", code)
	}
	if body != "pong" {
		t.Errorf("body=%q, want pong", body)
	}

	// 优雅关闭：应返回 nil，Serve 返回 ErrServerClosed。
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
	wg.Wait()
	if !errors.Is(serveErr, ErrServerClosed) {
		t.Errorf("Serve returned %v, want ErrServerClosed", serveErr)
	}
	if !srv.IsClosed() {
		t.Error("IsClosed should be true after Shutdown")
	}
}

// TestServer_EndToEnd_HTTPS 端到端验证 HTTPS：自签名证书→TLS 客户端请求→关闭。
func TestServer_EndToEnd_HTTPS(t *testing.T) {
	m := New()
	if err := m.RawHandle(http.MethodGet, "/secure", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.WriteString("tls-ok")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	cert, pool := selfSignedCert(t)
	ln := listenLocal(t)
	srv := NewServer(m, WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}}))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// TLSConfig 已注入，certFile/keyFile 可空。
		_ = srv.ServeTLS(ln, "", "")
	}()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "127.0.0.1"},
		},
		Timeout: 2 * time.Second,
	}
	url := "https://" + ln.Addr().String() + "/secure"
	body, code := getWithRetryClient(t, client, url)
	if code != http.StatusOK {
		t.Errorf("GET /secure status=%d, want 200", code)
	}
	if body != "tls-ok" {
		t.Errorf("body=%q, want tls-ok", body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
	wg.Wait()
}

// TestServer_ListenAndServeTLS_Validation 验证缺证书且无 TLSConfig 时报错。
func TestServer_ListenAndServeTLS_Validation(t *testing.T) {
	srv := NewServer(New())
	if err := srv.ListenAndServeTLS("", ""); err == nil {
		t.Error("ListenAndServeTLS without cert/key or TLSConfig should error")
	}
}

// TestServer_DoubleStart 验证重复启动被拒绝。
func TestServer_DoubleStart(t *testing.T) {
	ln := listenLocal(t)
	defer ln.Close()
	srv := NewServer(New())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.Serve(ln)
	}()
	// 等 Serve 抢到 started 标志。
	for i := 0; i < 100 && !srv.IsStarted(); i++ {
		time.Sleep(time.Millisecond)
	}
	ln2 := listenLocal(t)
	defer ln2.Close()
	if err := srv.Serve(ln2); err == nil {
		t.Error("second Serve should be rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	wg.Wait()
}

// --- 测试辅助 ---------------------------------------------------------------

// getWithRetry 在服务器刚起时可能连不上，短暂重试。
func getWithRetry(t *testing.T, url string, client *http.Client) (string, int) {
	t.Helper()
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return getWithRetryClient(t, client, url)
}

func getWithRetryClient(t *testing.T, client *http.Client, url string) (string, int) {
	t.Helper()
	var lastErr error
	for i := 0; i < 50; i++ {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(10 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b), resp.StatusCode
	}
	t.Fatalf("GET %s failed after retries: %v", url, lastErr)
	return "", 0
}

// selfSignedCert 生成内存自签名 ECDSA 证书（CN/SAN=127.0.0.1）与信任池。
func selfSignedCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("createcert: %v", err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsecert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return cert, pool
}
