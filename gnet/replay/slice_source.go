package replay

import (
	"io"

	"github.com/sofiworker/gk/gnet/packet"
)

// SliceSource 是可重复读取的内存报文来源（循环回放用）：流尽后自动
// 重置到开头并返回 io.EOF，供 Replayer 的 Loop 配置重新读取。
//
// SliceSource is a re-readable in-memory packet source for looped replay:
// on exhaustion it resets to the start and returns io.EOF so the Replayer's
// Loop option can read it again.
type SliceSource struct {
	pkts []*packet.Packet
	pos  int
}

// NewSliceSource 创建切片来源。
// NewSliceSource creates a slice source.
func NewSliceSource(pkts ...*packet.Packet) *SliceSource {
	return &SliceSource{pkts: pkts}
}

// Next 返回下一个报文；流尽时重置并返回 io.EOF。
// Next returns the next packet; resets and returns io.EOF at the end.
func (s *SliceSource) Next() (*packet.Packet, error) {
	if s.pos >= len(s.pkts) {
		s.pos = 0
		return nil, io.EOF
	}
	p := s.pkts[s.pos]
	s.pos++
	return p, nil
}
