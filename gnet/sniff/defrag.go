package sniff

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sofiworker/gk/gnet/layers"
)

// Defragmenter 做 IPv4 分片重组：按 (src, dst, id, proto) 聚合分片，
// 任意分片到达时检查连续性，全部到齐即输出重组后的传输层载荷。
//
// Defragmenter performs IPv4 fragment reassembly: fragments aggregate by
// (src, dst, id, proto); every arrival triggers a contiguity check and the
// reassembled transport payload is emitted once complete.
type Defragmenter struct {
	mu     sync.Mutex
	groups map[string]*fragGroup
}

// fragGroup 是一个分片组。
// fragGroup is one fragment group.
type fragGroup struct {
	segs map[uint16][]byte // offset → 数据
	end  int               // 最后一片的结束位置（未到 = -1）
	last time.Time
}

// NewDefragmenter 创建分片重组器。
// NewDefragmenter creates a defragmenter.
func NewDefragmenter() *Defragmenter {
	return &Defragmenter{groups: make(map[string]*fragGroup)}
}

// Push 交付一个 IPv4 分片；未分片的包直接透传。返回 (完整载荷, 是否
// 重组完成)。
//
// Push delivers one IPv4 fragment; unfragmented packets pass through.
// Returns (full payload, reassembled).
func (d *Defragmenter) Push(ip *layers.IPv4) ([]byte, bool) {
	offset := int(ip.FragOffset) * 8
	more := ip.Flags&0x01 != 0
	data := ip.Payload()
	if offset == 0 && !more {
		return data, true // 未分片：直接透传
	}
	key := fmt.Sprintf("%s|%s|%d|%d", ip.SrcIP, ip.DstIP, ip.ID, ip.Protocol)

	d.mu.Lock()
	defer d.mu.Unlock()
	g := d.groups[key]
	if g == nil {
		g = &fragGroup{segs: make(map[uint16][]byte), end: -1}
		d.groups[key] = g
	}
	g.last = time.Now()
	g.segs[uint16(offset)] = append([]byte(nil), data...)
	if !more {
		g.end = offset + len(data)
	}
	if g.end < 0 {
		return nil, false // 最后一片未到：等待
	}
	// 连续性检查：从 0 拼接到 end。
	var out []byte
	pos := 0
	for pos < g.end {
		seg, ok := g.segs[uint16(pos)]
		if !ok {
			return nil, false
		}
		out = append(out, seg...)
		pos += len(seg)
	}
	delete(d.groups, key)
	return out, true
}

// Expire 清理空闲超过 idle 的分片组，返回清理数。
// Expire drops groups idle for more than idle, returning the count.
func (d *Defragmenter) Expire(idle time.Duration) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	cutoff := time.Now().Add(-idle)
	n := 0
	for k, g := range d.groups {
		if g.last.Before(cutoff) {
			delete(d.groups, k)
			n++
		}
	}
	return n
}

// buildFragment 构造 IPv4 分片层（测试辅助）。
// buildFragment builds an IPv4 fragment layer (test helper).
func buildFragment(src, dst net.IP, id uint16, proto uint8, fragOffset uint16, more bool, data []byte) *layers.IPv4 {
	flags := uint8(0)
	if more {
		flags = 0x01 // MF（解码器语义：flags 字节右移 3 位后的值）
	}
	return &layers.IPv4{
		Version:    4,
		ID:         id,
		TTL:        64,
		Protocol:   proto,
		Flags:      flags,
		FragOffset: fragOffset,
		SrcIP:      src,
		DstIP:      dst,
		BaseLayer:  layers.BaseLayer{PayloadData: data},
	}
}
