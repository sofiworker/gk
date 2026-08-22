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
	"sync/atomic"
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
	srv := m

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
	cert, pool := selfSignedCert(t)
	m := New(WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}}))
	if err := m.RawHandle(http.MethodGet, "/secure", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.WriteString("tls-ok")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	ln := listenLocal(t)
	srv := m

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

// TestServer_RunTLS_Validation 验证缺证书且无 TLSConfig 时报错。
func TestServer_RunTLS_Validation(t *testing.T) {
	srv := New()
	if err := srv.RunTLS("", "", ""); err == nil {
		t.Error("RunTLS without cert/key or TLSConfig should error")
	}
}

// TestServer_DoubleStart 验证重复启动被拒绝。
func TestServer_DoubleStart(t *testing.T) {
	ln := listenLocal(t)
	defer ln.Close()
	srv := New()

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

// TestServer_RunAndShutdown 端到端验证一步式 Run 启动 + Shutdown 优雅关闭，并顺带在
// 真实 HTTP 上复核全局 CORS 对未注册 OPTIONS 预检生效。
// TestServer_RunAndShutdown end-to-end verifies the one-liner Run plus graceful
// Shutdown, and rechecks over real HTTP that global CORS answers preflight on an
// unregistered OPTIONS route.
func TestServer_RunAndShutdown(t *testing.T) {
	// 借一个真实空闲地址后立即释放，交给 Run 复用（窗口极小，端口刚释放）。
	ln := listenLocal(t)
	addr := ln.Addr().String()
	_ = ln.Close()

	s := New()
	s.Use(CORS(CORSDefault()))
	if err := s.RawHandle(http.MethodGet, "/ping", func(ctx context.Context, req *Request, resp *Response) error {
		_, _ = resp.WriteString("pong")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- s.Run(addr) }()

	body, code := getWithRetry(t, "http://"+addr+"/ping", nil)
	if code != http.StatusOK || body != "pong" {
		t.Errorf("GET /ping = %q [%d], want pong [200]", body, code)
	}

	preReq, _ := http.NewRequest(http.MethodOptions, "http://"+addr+"/ping", nil)
	preReq.Header.Set("Origin", "https://x.com")
	preReq.Header.Set("Access-Control-Request-Method", "GET")
	preResp, err := http.DefaultClient.Do(preReq)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	_ = preResp.Body.Close()
	if preResp.StatusCode != http.StatusNoContent {
		t.Errorf("preflight status=%d, want 204", preResp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, ErrServerClosed) {
			t.Errorf("Run returned %v, want ErrServerClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Run did not return after Shutdown")
	}
}

// TestServer_ConcurrentShutdownAllWaitForDrain 验证并发 Shutdown 的【每个】调用者都等到
// 进行中请求真正排空才返回。这锁定一个曾经的缺陷:若对重复调用提前 return nil,第二个
// 调用者会在尚未排空时拿到 nil、误判为已完成。
// TestServer_ConcurrentShutdownAllWaitForDrain verifies EVERY concurrent Shutdown
// caller waits for in-flight requests to actually drain. This locks down a past
// defect: short-circuiting a repeat call with nil handed the second caller a nil
// before the drain finished, misreporting completion.
func TestServer_ConcurrentShutdownAllWaitForDrain(t *testing.T) {
	const handlerHold = 400 * time.Millisecond

	var once sync.Once
	inFlight := make(chan struct{})
	s := New()
	if err := s.RawHandle(http.MethodGet, "/slow", func(ctx context.Context, req *Request, resp *Response) error {
		once.Do(func() { close(inFlight) })
		time.Sleep(handlerHold)
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	ln := listenLocal(t)
	go func() { _ = s.Serve(ln) }()

	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Get("http://" + ln.Addr().String() + "/slow"); err == nil {
			_ = resp.Body.Close()
		}
	}()

	// 等 handler 真正进入,确保 Shutdown 时确有进行中请求。
	select {
	case <-inFlight:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never started")
	}

	const callers = 3
	elapsed := make([]time.Duration, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			start := time.Now()
			_ = s.Shutdown(ctx)
			elapsed[idx] = time.Since(start)
		}(i)
	}
	wg.Wait()

	// 阈值取 handlerHold 的一半,留足余量避免抖动。
	want := handlerHold / 2
	for i, d := range elapsed {
		if d < want {
			t.Errorf("Shutdown caller %d returned after %v, want >= %v (must wait for drain)", i, d, want)
		}
	}
}

// TestServer_ListenFailure_AllowsRetryOnAnotherAddr 用真实的端口占用验证启动失败不污染状态：
// Server 仍可换到空闲地址成功服务。若失败后 state 卡在 running，重试会拿到 ErrServerStarted。
func TestServer_ListenFailure_AllowsRetryOnAnotherAddr(t *testing.T) {
	busy := listenLocal(t)
	defer func() { _ = busy.Close() }()

	s := New()
	if err := s.RawHandle(http.MethodGet, "/ping", func(ctx context.Context, req *Request, resp *Response) error {
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.WriteString("pong")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	err := s.Run(busy.Addr().String())
	if err == nil {
		t.Fatal("Run on an occupied port returned nil, want a bind error")
	}
	if errors.Is(err, ErrServerClosed) {
		t.Fatalf("Run returned %v, want a bind error", err)
	}
	if s.IsStarted() {
		t.Error("IsStarted() = true after a failed bind, want false")
	}

	// 观察 Serve 的返回值而非直接发请求：状态若被污染，Serve 会立刻返回 ErrServerStarted，
	// 而 ln 是个无人 Accept 的监听器——内核照样完成握手，客户端只会干等到超时，把一次本该
	// 立即失败的断言拖成漫长重试。
	ln := listenLocal(t)
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(ln) }()
	select {
	case err := <-serveErr:
		t.Fatalf("Serve after a failed bind returned %v, want it to start serving (state was corrupted)", err)
	case <-time.After(50 * time.Millisecond):
	}

	body, code := getWithRetry(t, "http://"+ln.Addr().String()+"/ping", nil)
	if code != http.StatusOK || body != "pong" {
		t.Errorf("after retry got %d %q, want 200 \"pong\"", code, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// TestServer_ShutdownDoesNotHoldLockWhileDraining 锁定一条防死锁性质：Shutdown 的阻塞排空必
// 须发生在释放 mu 之后。排空时长由业务 handler 决定（可达数十秒），若它在临界区内进行，期间
// 任何 IsStarted/IsClosed 查询都会被卡住——handler 内查询状态即构成事实上的死锁。
func TestServer_ShutdownDoesNotHoldLockWhileDraining(t *testing.T) {
	const handlerHold = 600 * time.Millisecond

	var once sync.Once
	inFlight := make(chan struct{})
	s := New()
	if err := s.RawHandle(http.MethodGet, "/slow", func(ctx context.Context, req *Request, resp *Response) error {
		once.Do(func() { close(inFlight) })
		time.Sleep(handlerHold)
		resp.WriteHeader(http.StatusOK)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	ln := listenLocal(t)
	go func() { _ = s.Serve(ln) }()

	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Get("http://" + ln.Addr().String() + "/slow"); err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-inFlight:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never started")
	}

	var drained atomic.Bool
	shutdownCalled := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		close(shutdownCalled)
		_ = s.Shutdown(ctx)
		drained.Store(true)
	}()

	// 不能靠 IsClosed() 判断排空是否已开始：若 mu 真被持有，这次查询本身就会被卡住，探测
	// 窗口反而消失、测试退化为 skip。改用独立信号加短暂让步，确保 Shutdown 已进入排空。
	<-shutdownCalled
	time.Sleep(20 * time.Millisecond)

	// Skip 判断必须在计时【之前】：若 mu 被持有，探测本身会耗尽整个排空期，事后再看
	// drained 必然为真，测试就会退化成永远 skip、放过它本要捕获的缺陷。
	if drained.Load() {
		t.Skip("drain finished before the probe window; timing too tight to assert")
	}

	// 排空进行中密集查询状态：不持锁时是纯内存操作，持锁则会被阻塞到排空结束。
	const probes = 1000
	start := time.Now()
	for i := 0; i < probes; i++ {
		_ = s.IsStarted()
		_ = s.IsClosed()
	}
	cost := time.Since(start)

	if want := handlerHold / 4; cost > want {
		t.Errorf("%d state queries during drain took %v, want < %v (mu must not be held while draining)", probes*2, cost, want)
	}

	// 等排空真正结束：既确认 Shutdown 最终返回，也避免测试提前退出留下后台 goroutine。
	deadline := time.Now().Add(5 * time.Second)
	for !drained.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !drained.Load() {
		t.Error("Shutdown did not return after the in-flight request finished")
	}
	if !s.IsClosed() {
		t.Error("IsClosed() = false after Shutdown returned, want true")
	}
}
