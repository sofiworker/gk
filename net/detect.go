package gnet

import (
	"errors"
	"io"
	"net"
)

// NamedMatcher 是协议识别器：名称 + 首字节匹配函数。
// NamedMatcher is a protocol detector: a name plus a head-bytes match func.
type NamedMatcher struct {
	Name  string
	Match func(head []byte) bool
}

// ErrNoProtocolMatch 表示没有识别器匹配。
// ErrNoProtocolMatch means no matcher matched.
var ErrNoProtocolMatch = errors.New("gnet: no protocol matcher matched")

// MatchTLS 识别 TLS ClientHello：记录层首字节为 0x16。
// MatchTLS detects a TLS ClientHello: the first record byte is 0x16.
func MatchTLS(head []byte) bool {
	return len(head) > 0 && head[0] == 0x16
}

// MatchHTTP 识别 HTTP/1.x 方法前缀。
// MatchHTTP detects HTTP/1.x method prefixes.
func MatchHTTP(head []byte) bool {
	methods := []string{"GET ", "POST ", "PUT ", "DELETE ", "HEAD ", "OPTIONS ", "PATCH ", "CONNECT ", "TRACE "}
	for _, m := range methods {
		if len(head) >= len(m) && string(head[:len(m)]) == m {
			return true
		}
	}
	return false
}

// DetectProtocol 窥探连接首字节并依次匹配识别器；返回匹配名与回灌了
// 已读字节的新连接。未匹配时返回 ErrNoProtocolMatch，连接仍可继续使用。
//
// DetectProtocol peeks the first bytes of conn and matches them against the
// matchers in order; it returns the matched name and a new conn with the read
// bytes replayed. On no match it returns ErrNoProtocolMatch and the conn stays
// usable.
func DetectProtocol(conn net.Conn, peek int, matchers []NamedMatcher) (string, net.Conn, error) {
	if peek < 1 {
		peek = 1
	}
	head := make([]byte, peek)
	n, err := io.ReadFull(conn, head)
	head = head[:n]
	wrapped := &prefixedConn{Conn: conn, prefix: head}
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", wrapped, err
	}
	for _, m := range matchers {
		if m.Match(head) {
			return m.Name, wrapped, nil
		}
	}
	return "", wrapped, ErrNoProtocolMatch
}

// prefixedConn 是带前缀回灌的连接包装。
// prefixedConn wraps a conn and replays the peeked prefix on Read.
type prefixedConn struct {
	net.Conn
	prefix []byte
}

// Read 先回灌前缀再读底层连接。
// Read replays the prefix before reading the underlying conn.
func (c *prefixedConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}
