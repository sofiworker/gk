package forward

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// UDPSession 表示一个UDP会话
type UDPSession struct {
	localAddr  *net.UDPAddr
	remoteAddr *net.UDPAddr
	lastActive time.Time
	dataChan   chan []byte
	closeChan  chan struct{}
}

// UDPBridge UDP双向桥接
type UDPBridge struct {
	localConn  *net.UDPConn
	remoteAddr *net.UDPAddr
	bufferSize int
	logger     *log.Logger
	mu         sync.RWMutex
	closed     bool
	sessions   map[string]*UDPSession // key: client_addr->server_addr
	sessionTTL time.Duration
}

// UDPConfig UDP桥接配置
type UDPConfig struct {
	BufferSize int           // 缓冲区大小
	Logger     *log.Logger   // 日志记录器
	SessionTTL time.Duration // 会话超时时间
}

// NewUDPBridge 创建新的UDP桥接实例
func NewUDPBridge(localConn *net.UDPConn, remoteAddr string, config ...UDPConfig) (*UDPBridge, error) {
	// 解析远程地址
	raddr, err := net.ResolveUDPAddr("udp", remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve remote address: %v", err)
	}

	bridge := &UDPBridge{
		localConn:  localConn,
		remoteAddr: raddr,
		bufferSize: 4096,
		sessions:   make(map[string]*UDPSession),
		sessionTTL: 5 * time.Minute, // 默认5分钟超时
	}

	// 应用配置
	if len(config) > 0 {
		cfg := config[0]
		if cfg.BufferSize > 0 {
			bridge.bufferSize = cfg.BufferSize
		}
		bridge.logger = cfg.Logger
		if cfg.SessionTTL > 0 {
			bridge.sessionTTL = cfg.SessionTTL
		}
	}

	// 如果没有提供日志记录器，使用默认的
	if bridge.logger == nil {
		bridge.logger = log.Default()
	}

	return bridge, nil
}

// Start 启动UDP双向桥接
func (b *UDPBridge) Start() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return io.ErrClosedPipe
	}
	b.mu.Unlock()

	b.logger.Printf("Starting UDP bridge: %s -> %s", b.localConn.LocalAddr(), b.remoteAddr)

	// 启动会话清理器
	go b.sessionCleaner()

	// 启动数据接收循环
	for {
		b.mu.RLock()
		if b.closed {
			b.mu.RUnlock()
			return nil
		}
		b.mu.RUnlock()

		buffer := make([]byte, b.bufferSize)
		n, clientAddr, err := b.localConn.ReadFromUDP(buffer)
		if err != nil {
			b.mu.RLock()
			closed := b.closed
			b.mu.RUnlock()

			if closed {
				return nil
			}
			b.logger.Printf("ReadFromUDP error: %v", err)
			continue
		}

		if n == 0 {
			continue
		}

		b.logger.Printf("Received %d bytes from %s", n, clientAddr)

		// 处理接收到的数据
		go b.handleIncomingData(buffer[:n], clientAddr)
	}
}

// handleIncomingData 处理接收到的数据
func (b *UDPBridge) handleIncomingData(data []byte, clientAddr *net.UDPAddr) {
	sessionKey := fmt.Sprintf("%s->%s", clientAddr.String(), b.remoteAddr.String())

	b.mu.Lock()
	session, exists := b.sessions[sessionKey]
	if !exists {
		// 创建新会话
		session = &UDPSession{
			localAddr:  clientAddr,
			remoteAddr: b.remoteAddr,
			lastActive: time.Now(),
			dataChan:   make(chan []byte, 100), // 缓冲通道
			closeChan:  make(chan struct{}),
		}
		b.sessions[sessionKey] = session

		// 启动远程数据接收
		go b.startRemoteReceiver(session)

		b.logger.Printf("New UDP session created: %s", sessionKey)
	} else {
		session.lastActive = time.Now()
	}
	b.mu.Unlock()

	// 发送数据到远程
	if exists {
		select {
		case session.dataChan <- data:
			// 数据成功发送到通道
		default:
			b.logger.Printf("Session channel full, dropping data: %s", sessionKey)
		}
	} else {
		// 对于新会话，直接发送第一包数据
		go b.forwardToRemote(data, session)
	}
}

// forwardToRemote 转发数据到远程
func (b *UDPBridge) forwardToRemote(data []byte, session *UDPSession) {
	_, err := b.localConn.WriteToUDP(data, session.remoteAddr)
	if err != nil {
		b.logger.Printf("WriteToUDP error to %s: %v", session.remoteAddr, err)
		return
	}
	b.logger.Printf("Forwarded %d bytes to %s", len(data), session.remoteAddr)
}

// startRemoteReceiver 启动远程数据接收器
func (b *UDPBridge) startRemoteReceiver(session *UDPSession) {
	// 注意：UDP是无连接的，我们需要模拟会话
	// 这里我们定期检查会话是否活跃

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case data := <-session.dataChan:
			b.forwardToRemote(data, session)
			session.lastActive = time.Now()

		case <-ticker.C:
			// 检查会话是否超时
			b.mu.Lock()
			if time.Since(session.lastActive) > b.sessionTTL {
				delete(b.sessions, fmt.Sprintf("%s->%s", session.localAddr.String(), session.remoteAddr.String()))
				close(session.closeChan)
				b.mu.Unlock()
				b.logger.Printf("UDP session expired: %s->%s", session.localAddr, session.remoteAddr)
				return
			}
			b.mu.Unlock()

		case <-session.closeChan:
			return
		}
	}
}

// sessionCleaner 清理过期会话
func (b *UDPBridge) sessionCleaner() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.mu.Lock()
			if b.closed {
				b.mu.Unlock()
				return
			}

			now := time.Now()
			expiredSessions := make([]string, 0)

			for key, session := range b.sessions {
				if now.Sub(session.lastActive) > b.sessionTTL {
					expiredSessions = append(expiredSessions, key)
					close(session.closeChan)
				}
			}

			for _, key := range expiredSessions {
				delete(b.sessions, key)
				b.logger.Printf("Cleaned expired session: %s", key)
			}
			b.mu.Unlock()
		}
	}
}

// Close 关闭UDP桥接
func (b *UDPBridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	b.closed = true
	b.logger.Println("Closing UDP bridge...")

	// 关闭所有会话
	for key, session := range b.sessions {
		close(session.closeChan)
		delete(b.sessions, key)
	}

	if b.localConn != nil {
		return b.localConn.Close()
	}

	return nil
}

// GetActiveSessions 获取活动会话数量
func (b *UDPBridge) GetActiveSessions() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.sessions)
}
