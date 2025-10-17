package gnet

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// ProtocolAwareBridge 支持协议感知的桥接（包括大小端转换）
type ProtocolAwareBridge struct {
	localConn  net.Conn
	remoteConn net.Conn
	bufferSize int
	logger     *log.Logger
	mu         sync.Mutex
	closed     bool

	// 协议处理相关字段
	byteOrder       binary.ByteOrder // 字节序配置
	protocolHandler ProtocolHandler  // 协议处理器
}

// TCPConfig 桥接配置
type TCPConfig struct {
	BufferSize int           // 缓冲区大小
	Logger     *log.Logger   // 日志记录器
	Timeout    time.Duration // 超时时间
}

// ProtocolHandler 协议处理器接口
type ProtocolHandler interface {
	// ProcessData 处理数据，可以进行大小端转换等操作
	ProcessData(data []byte, direction Direction) []byte
	// ShouldProcess 判断是否需要处理这种数据
	ShouldProcess(data []byte) bool
}

// Direction 数据传输方向
type Direction int

const (
	LocalToRemote Direction = iota
	RemoteToLocal
)

// ByteOrderConfig 字节序配置
type ByteOrderConfig struct {
	LocalToRemoteOrder binary.ByteOrder // 本地到远程的字节序
	RemoteToLocalOrder binary.ByteOrder // 远程到本地的字节序
	ProtocolHandler    ProtocolHandler  // 协议处理器
}

// NewProtocolAwareBridge 创建协议感知的桥接
func NewProtocolAwareBridge(localConn, remoteConn net.Conn, config TCPConfig, byteOrderConfig *ByteOrderConfig) *ProtocolAwareBridge {
	bridge := &ProtocolAwareBridge{
		localConn:  localConn,
		remoteConn: remoteConn,
		bufferSize: config.BufferSize,
		logger:     config.Logger,
		byteOrder:  binary.BigEndian, // 默认大端序
	}

	if byteOrderConfig != nil {
		if byteOrderConfig.ProtocolHandler != nil {
			bridge.protocolHandler = byteOrderConfig.ProtocolHandler
		}
	}

	if bridge.logger == nil {
		bridge.logger = log.Default()
	}

	return bridge
}

// 示例协议处理器：处理包含16位和32位整数的协议
type SimpleProtocolHandler struct {
	localByteOrder  binary.ByteOrder
	remoteByteOrder binary.ByteOrder
}

func NewSimpleProtocolHandler(localOrder, remoteOrder binary.ByteOrder) *SimpleProtocolHandler {
	return &SimpleProtocolHandler{
		localByteOrder:  localOrder,
		remoteByteOrder: remoteOrder,
	}
}

func (h *SimpleProtocolHandler) ShouldProcess(data []byte) bool {
	// 简单的判断：数据长度至少包含一个16位整数
	return len(data) >= 2
}

func (h *SimpleProtocolHandler) ProcessData(data []byte, direction Direction) []byte {
	if len(data) < 2 {
		return data
	}

	var result []byte

	switch direction {
	case LocalToRemote:
		// 本地到远程：如果需要，进行字节序转换
		result = h.convertByteOrder(data, h.localByteOrder, h.remoteByteOrder)
	case RemoteToLocal:
		// 远程到本地：如果需要，进行字节序转换
		result = h.convertByteOrder(data, h.remoteByteOrder, h.localByteOrder)
	default:
		result = data
	}

	return result
}

func (h *SimpleProtocolHandler) convertByteOrder(data []byte, from, to binary.ByteOrder) []byte {
	if from == to {
		return data // 字节序相同，不需要转换
	}

	// 这里实现具体的协议解析和字节序转换
	// 示例：假设协议格式为 [16位命令][32位数据长度][数据]
	if len(data) >= 6 { // 至少包含16位命令 + 32位长度
		result := make([]byte, len(data))
		copy(result, data)

		// 转换16位命令
		cmd := from.Uint16(data[0:2])
		to.PutUint16(result[0:2], cmd)

		// 转换32位数据长度
		length := from.Uint32(data[2:6])
		to.PutUint32(result[2:6], length)

		return result
	}

	return data
}

// Start 启动协议感知桥接
func (b *ProtocolAwareBridge) Start() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return io.ErrClosedPipe
	}
	b.mu.Unlock()

	b.logger.Println("Starting protocol-aware TCP bridge...")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		b.forwardWithProtocol(b.localConn, b.remoteConn, LocalToRemote)
	}()

	go func() {
		defer wg.Done()
		b.forwardWithProtocol(b.remoteConn, b.localConn, RemoteToLocal)
	}()

	wg.Wait()
	b.Close()

	return nil
}

// forwardWithProtocol 带协议处理的数据转发
func (b *ProtocolAwareBridge) forwardWithProtocol(src, dst net.Conn, direction Direction) {
	buffer := make([]byte, b.bufferSize)

	for {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return
		}
		b.mu.Unlock()

		n, err := src.Read(buffer)
		if err != nil {
			if err != io.EOF {
				b.logger.Printf("Read error: %v", err)
			}
			break
		}

		if n == 0 {
			continue
		}

		data := buffer[:n]

		// 如果有协议处理器，处理数据
		if b.protocolHandler != nil && b.protocolHandler.ShouldProcess(data) {
			processedData := b.protocolHandler.ProcessData(data, direction)
			if len(processedData) != len(data) {
				b.logger.Printf("Warning: Protocol handler changed data length from %d to %d", len(data), len(processedData))
			}
			data = processedData
		}

		b.logger.Printf("Forwarding %d bytes: %v", len(data), direction)

		_, err = dst.Write(data)
		if err != nil {
			b.logger.Printf("Write error: %v", err)
			break
		}
	}
}

// Close 关闭桥接
func (b *ProtocolAwareBridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	b.closed = true
	b.logger.Println("Closing protocol-aware TCP bridge...")

	var errs []error

	if err := b.localConn.Close(); err != nil {
		errs = append(errs, err)
	}

	if err := b.remoteConn.Close(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errs[0]
	}

	return nil
}
