package ghttp

import (
	"context"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// ===========================================================================
// WebSocket:基于 github.com/gorilla/websocket 的薄语法糖。
// ghttp 只负责让 *Response 可被 Hijack(见 Response.Hijack),协议实现委托给 gorilla。
// WSUpgrader 用仓库惯用的 With* 选项包装 websocket.Upgrader;Upgrade 在 RawHandle 内
// 完成握手升级并返回 *websocket.Conn;ServeWS 进一步注册一个 GET 端点,升级成功后把连接
// 交给业务处理器并在其返回时自动关闭——把"注册 + 升级 + 收尾"收敛成一行。
// WebSocket: a thin sugar layer over github.com/gorilla/websocket. ghttp only
// makes *Response hijackable (see Response.Hijack); the protocol is delegated to
// gorilla. WSUpgrader wraps websocket.Upgrader with the repo's With* options;
// Upgrade performs the handshake inside a RawHandle and returns a *websocket.Conn;
// ServeWS additionally registers a GET endpoint, hands the connection to a
// business handler on success, and closes it on return — collapsing
// "register + upgrade + teardown" into one call.
// ===========================================================================

// WSHandlerFunc 是 WebSocket 业务处理器：升级成功后被调用，拥有 conn 的读写与生命周期。
// 返回的 error 仅用于观测 (此时 HTTP 响应已在升级时提交，无法改写);ServeWS 会在它返回
// 后关闭 conn。
//
// 注意:升级后的连接是 hijacked 状态，由应用自行管理生命周期;server.Shutdown 不会自动
// 取消该连接的 context，也不会等待其排空。若需要优雅关闭，应在业务逻辑中监听外部信号
// 或使用上下文键传递“停止条件”，并在 handler 内检测退出。
//
// WSHandlerFunc is the WebSocket business handler: called after a successful
// upgrade, owning the conn's I/O and lifecycle. The returned error is for
// observation only (the HTTP response was committed at upgrade and cannot be
// rewritten); ServeWS closes conn after it returns.
//
// Note: After upgrade the connection is hijacked and its lifecycle is managed by the
// application; server.Shutdown does NOT automatically cancel that connection's context
// nor wait for it to drain. For graceful shutdown, listen for external signals in your
// business logic or pass "stop conditions" via context keys, and exit the handler when
// detected.
type WSHandlerFunc func(ctx context.Context, req *Request, conn *websocket.Conn) error

// WSUpgrader 承载一个 websocket.Upgrader,把 HTTP 连接升级为 WebSocket。经 NewWSUpgrader
// 构造,可复用(并发安全,gorilla.Upgrader 本身无每连接状态)。
// WSUpgrader holds a websocket.Upgrader that upgrades an HTTP connection to
// WebSocket. Construct it via NewWSUpgrader; it is reusable (concurrency-safe, as
// gorilla.Upgrader carries no per-connection state).
type WSUpgrader struct {
	up        websocket.Upgrader
	readLimit int64
}

// defaultWSReadLimit 是升级后对单条消息的字节上限。gorilla 默认【不限】读长度，于是任意
// 已连接的客户端都能以持续大帧把服务端内存打满（这是 WS 最常见的 DoS 面之一），而本包
// 此前没有任何设置点。取 1 MiB：足以覆盖控制面/JSON 消息，又能挡住无界洪泛。
// defaultWSReadLimit caps a single message's size after the upgrade. gorilla places
// no read limit by default, so any connected client can exhaust server memory with
// ever-larger frames (one of the most common WS DoS surfaces), and this package
// previously offered no place to set it. 1 MiB covers control/JSON messages while
// blocking unbounded flooding.
const defaultWSReadLimit = 1 << 20

// WSOption 配置 WSUpgrader。
// WSOption configures a WSUpgrader.
type WSOption func(*WSUpgrader)

// NewWSUpgrader 构造一个 WSUpgrader 并应用选项。默认沿用 gorilla 的安全 CheckOrigin
// (拒绝跨源:仅放行同源或无 Origin 头的请求);跨源需经 WithWSCheckOrigin 显式放开。
// NewWSUpgrader builds a WSUpgrader and applies options. It keeps gorilla's safe
// default CheckOrigin (rejects cross-origin: only same-origin or Origin-less
// requests pass); allow cross-origin explicitly via WithWSCheckOrigin.
func NewWSUpgrader(opts ...WSOption) *WSUpgrader {
	u := &WSUpgrader{readLimit: defaultWSReadLimit}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

// WithWSCheckOrigin 设置跨源校验:返回 true 才接受该 Origin 的升级请求。传 nil 恢复
// gorilla 的安全默认(仅同源)。生产环境应据允许的前端域名审慎实现,勿无脑返回 true。
// WithWSCheckOrigin sets origin validation: only a true result accepts the
// upgrade for that Origin. Passing nil restores gorilla's safe default
// (same-origin only). Implement it carefully against allowed frontend origins in
// production; do not blindly return true.
func WithWSCheckOrigin(fn func(r *http.Request) bool) WSOption {
	return func(u *WSUpgrader) { u.up.CheckOrigin = fn }
}

// WithWSReadBufferSize 设置读缓冲字节数(0 表示使用 gorilla 默认)。
// WithWSReadBufferSize sets the read buffer size in bytes (0 uses gorilla's
// default).
func WithWSReadBufferSize(n int) WSOption {
	return func(u *WSUpgrader) { u.up.ReadBufferSize = n }
}

// WithWSWriteBufferSize 设置写缓冲字节数(0 表示使用 gorilla 默认)。
// WithWSWriteBufferSize sets the write buffer size in bytes (0 uses gorilla's
// default).
func WithWSWriteBufferSize(n int) WSOption {
	return func(u *WSUpgrader) { u.up.WriteBufferSize = n }
}

// WithWSSubprotocols 声明服务端按优先级支持的子协议,握手时与客户端 Sec-WebSocket-Protocol
// 协商。
// WithWSSubprotocols declares the server's supported subprotocols in priority
// order, negotiated against the client's Sec-WebSocket-Protocol at handshake.
func WithWSSubprotocols(protocols ...string) WSOption {
	return func(u *WSUpgrader) { u.up.Subprotocols = protocols }
}

// WithWSHandshakeTimeout 设置握手完成的超时时间(0 表示不限制)。
// WithWSHandshakeTimeout sets the handshake completion timeout (0 means no limit).
func WithWSHandshakeTimeout(d time.Duration) WSOption {
	return func(u *WSUpgrader) { u.up.HandshakeTimeout = d }
}

// WithWSCompression 启用 per-message-deflate 压缩协商(能否生效取决于客户端)。
// WithWSCompression enables per-message-deflate compression negotiation (whether
// it takes effect depends on the client).
func WithWSCompression(enabled bool) WSOption {
	return func(u *WSUpgrader) { u.up.EnableCompression = enabled }
}

// WithWSReadLimit 设置升级后单条消息的字节上限；传 0 或负值表示【不限】（显式退回
// gorilla 默认行为）。该上限是读侧唯一的内存防线，改大前需确认消息尺寸分布。
// WithWSReadLimit sets the per-message byte cap after the upgrade; 0 or a negative
// value means NO cap, explicitly opting back into gorilla's default. This limit is
// the only memory defense on the read side, so raise it only with a known message
// size distribution.
func WithWSReadLimit(n int64) WSOption {
	return func(u *WSUpgrader) { u.readLimit = n }
}

// Upgrade 在 RawHandle 内把 resp/req 升级为 WebSocket 连接。成功时 resp 已被 Hijack、
// 握手响应已写出,返回的 *websocket.Conn 归调用方所有(需自行 Close);失败时 gorilla
// 已写出相应的 HTTP 错误响应,调用方通常直接返回 err 即可。responseHeader 可携带随
// 101 一并返回的额外响应头(如 Set-Cookie),无则传 nil。
// Upgrade turns resp/req into a WebSocket connection inside a RawHandle. On
// success resp is hijacked, the handshake response is written, and the returned
// *websocket.Conn is owned by the caller (who must Close it); on failure gorilla
// has already written the corresponding HTTP error, so the caller usually just
// returns err. responseHeader may carry extra headers returned with the 101 (e.g.
// Set-Cookie); pass nil for none.
func (u *WSUpgrader) Upgrade(resp *Response, req *Request, responseHeader http.Header) (*websocket.Conn, error) {
	conn, err := u.up.Upgrade(resp, req.Request, responseHeader)
	if err != nil {
		return nil, err
	}
	// 读上限必须在升级成功后逐连接设置（gorilla 的限制是连接级而非 Upgrader 级）。
	// The read limit is per-connection in gorilla, so it must be set after the
	// upgrade rather than on the Upgrader.
	conn.SetReadLimit(u.readLimit)
	return conn, nil
}

// ServeWS 注册一个 GET 端点作为 WebSocket 升级点:请求到达时用 up 升级,成功则调用
// handler 处理连接、并在其返回后自动 conn.Close();升级失败时 gorilla 已写出 HTTP 错误。
// 它是 RawHandle + Upgrade 的语法糖,把注册、升级、收尾收敛成一行。up 为 nil 时使用
// 一个默认 WSUpgrader(安全的同源 CheckOrigin)。
// ServeWS registers a GET endpoint as a WebSocket upgrade point: on a request it
// upgrades with up, and on success invokes handler for the connection then
// conn.Close()s it after handler returns; on a failed upgrade gorilla has already
// written the HTTP error. It is sugar over RawHandle + Upgrade, collapsing
// register/upgrade/teardown into one call. A nil up uses a default WSUpgrader
// (safe same-origin CheckOrigin).
func ServeWS(r router, path string, up *WSUpgrader, handler WSHandlerFunc) error {
	if up == nil {
		up = NewWSUpgrader()
	}
	if err := r.register(http.MethodGet, path, Handler(func(ctx context.Context, req *Request, resp *Response) error {
		conn, err := up.Upgrade(resp, req, nil)
		if err != nil {
			// 升级失败:gorilla 已写出 HTTP 错误响应(4xx),此处仅把错误上抛给错误钩子
			// 观测。响应已提交,统一错误链不会改写。
			// Upgrade failed: gorilla already wrote the HTTP error (4xx); surface it
			// to the error hook for observation. The response is committed, so the
			// unified error chain will not rewrite it.
			return err
		}
		defer func() { _ = conn.Close() }()
		return handler(ctx, req, conn)
	})); err != nil {
		return err
	}
	// 登记进 OpenAPI 文档。WS 端点的响应形状不由框架决定(升级后走的是 WS 帧而非 HTTP
	// 响应体),故与 RawHandle 同样按 raw 记录:spec 只声明"这个路径存在且是 GET",不编造
	// 契约。漏掉这一步会让 WS 路径从 spec 中消失,与 RawHandle"路径存在即出现"的口径不一致。
	// Record it in the OpenAPI docs. A WS endpoint's response shape is not the
	// framework's to state (after the upgrade it speaks WS frames, not an HTTP body), so
	// it is recorded as raw just like RawHandle: the spec declares only that this GET
	// path exists, inventing no contract. Skipping this would drop WS paths from the
	// spec, inconsistent with RawHandle's "a path that exists shows up" rule.
	r.owner().noteRoute(r, http.MethodGet, path, routeDoc{raw: true})
	return nil
}
