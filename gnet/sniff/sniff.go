// Package sniff 提供流量嗅探管线：Source（报文来源）→ FlowTracker（五元组
// 流表）→ Reassembler（TCP 顺序重组）→ Analyzer（回调），对齐
// Suricata/Zeek 的处理管线简化版。设计见
// docs/superpowers/specs/2026-08-16-gnet-network-foundation-redesign.md §5.6。
//
// Package sniff provides the traffic sniffing pipeline: Source (packet
// origin) → FlowTracker (5-tuple flow table) → Reassembler (TCP in-order
// reassembly) → Analyzer (callbacks), a simplified Suricata/Zeek pipeline.
// See the gnet redesign spec §5.6.
package sniff

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/sofiworker/gk/gnet/layers"
	"github.com/sofiworker/gk/gnet/packet"
	"github.com/sofiworker/gk/gnet/pcap"
	"github.com/sofiworker/gk/gnet/pcapng"
)

// Source 是报文来源；流尽返回 io.EOF。
// Source is a packet origin; exhaustion returns io.EOF.
type Source interface {
	Next() (*packet.Packet, error)
}

// Op 是流事件类型。
// Op is the flow event kind.
type Op int

// 流事件。
// Flow event kinds.
const (
	OpNew Op = iota
	OpData
	OpEnd
)

// FlowKey 是五元组流标识。
// FlowKey is the 5-tuple flow identity.
type FlowKey struct {
	SrcIP, DstIP     net.IP
	SrcPort, DstPort uint16
	Proto            uint8
}

// Reverse 返回反向五元组（回程方向）。
// Reverse returns the reversed 5-tuple (return direction).
func (k FlowKey) Reverse() FlowKey {
	return FlowKey{SrcIP: k.DstIP, DstIP: k.SrcIP, SrcPort: k.DstPort, DstPort: k.SrcPort, Proto: k.Proto}
}

// Flow 是流表条目。
// Flow is a flow-table entry.
type Flow struct {
	Key      FlowKey
	Created  time.Time
	LastSeen time.Time
	Bytes    int64
	Packets  int64
}

// FlowTracker 按五元组聚合流。
// FlowTracker aggregates flows by 5-tuple.
type FlowTracker struct {
	mu    sync.Mutex
	flows map[string]*Flow
}

// NewFlowTracker 创建流表。
// NewFlowTracker creates a flow table.
func NewFlowTracker() *FlowTracker {
	return &FlowTracker{flows: make(map[string]*Flow)}
}

// Track 记录一个报文并返回其所属流（新建流返回 *Flow 且 op 为新建）。
// Track records a packet and returns its flow (plus a new-flow flag).
func (t *FlowTracker) Track(p *packet.Packet) (*Flow, bool) {
	nl, tl := p.NetworkLayer(), p.TransportLayer()
	key, ok := flowKeyOf(nl, tl)
	if !ok {
		return nil, false
	}
	f, isNew := t.trackKey(key, p.Timestamp)
	t.mu.Lock()
	f.Bytes += int64(len(p.Data))
	f.Packets++
	t.mu.Unlock()
	return f, isNew
}

// trackKey 记录一个五元组并返回其流与是否新建。
// trackKey records a 5-tuple and returns its flow plus a new-flow flag.
func (t *FlowTracker) trackKey(key FlowKey, ts time.Time) (*Flow, bool) {
	if ts.IsZero() {
		ts = time.Now()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	f, exists := t.flows[key.String()]
	if !exists {
		f = &Flow{Key: key, Created: ts}
		t.flows[key.String()] = f
	}
	f.LastSeen = ts
	return f, !exists
}

// Flows 返回当前全部流。
// Flows returns all current flows.
func (t *FlowTracker) Flows() []*Flow {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*Flow, 0, len(t.flows))
	for _, f := range t.flows {
		out = append(out, f)
	}
	return out
}

// Expire 移除并返回空闲超过 idle 的流。
// Expire removes and returns flows idle for more than idle.
func (t *FlowTracker) Expire(idle time.Duration) []*Flow {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := time.Now().Add(-idle)
	var out []*Flow
	for k, f := range t.flows {
		if f.LastSeen.Before(cutoff) {
			out = append(out, f)
			delete(t.flows, k)
		}
	}
	return out
}

// flowKeyOf 从网络层与传输层提取五元组。
// flowKeyOf extracts the 5-tuple from network and transport layers.
func flowKeyOf(nl, tl layers.Layer) (FlowKey, bool) {
	var key FlowKey
	switch n := nl.(type) {
	case *layers.IPv4:
		key.SrcIP, key.DstIP = n.SrcIP, n.DstIP
		key.Proto = n.Protocol
	case *layers.IPv6:
		key.SrcIP, key.DstIP = n.SrcIP, n.DstIP
		key.Proto = n.NextHeader
	default:
		return key, false
	}
	switch t := tl.(type) {
	case *layers.TCP:
		key.SrcPort, key.DstPort = t.SrcPort, t.DstPort
	case *layers.UDP:
		key.SrcPort, key.DstPort = t.SrcPort, t.DstPort
	default:
		return key, false
	}
	return key, true
}

// Reassembler 按方向做 TCP 顺序重组：连续段立即交付，乱序段暂存。
// Reassembler performs per-direction TCP in-order reassembly: contiguous
// segments deliver immediately, out-of-order segments are buffered.
type Reassembler struct {
	mu     sync.Mutex
	states map[string]*streamState
}

type streamState struct {
	next    uint32
	init    bool
	pending map[uint32][]byte
}

// NewReassembler 创建重组器。
// NewReassembler creates a reassembler.
func NewReassembler() *Reassembler {
	return &Reassembler{states: make(map[string]*streamState)}
}

// Push 交付一个方向的 TCP 段，返回可连续交付的负载序列。
// Push delivers one directional TCP segment, returning the contiguous
// payloads ready for delivery.
func (r *Reassembler) Push(key FlowKey, dir bool, seq uint32, data []byte) [][]byte {
	id := directionKey(key, dir)
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.states[id]
	if st == nil {
		st = &streamState{pending: make(map[uint32][]byte)}
		r.states[id] = st
	}
	if !st.init {
		st.next = seq
		st.init = true
	}
	var out [][]byte
	if seq == st.next {
		out = append(out, data)
		st.next = seq + uint32(len(data))
		// 排空连续暂存段。
		for {
			buf, ok := st.pending[st.next]
			if !ok {
				break
			}
			delete(st.pending, st.next)
			out = append(out, buf)
			st.next += uint32(len(buf))
		}
	} else if seq > st.next {
		st.pending[seq] = data
	}
	// seq < next：重叠旧段，静默丢弃。
	return out
}

// Close 移除方向状态（流结束）。
// Close removes the directional state (flow end).
func (r *Reassembler) Close(key FlowKey, dir bool) {
	id := directionKey(key, dir)
	r.mu.Lock()
	delete(r.states, id)
	r.mu.Unlock()
}

func directionKey(k FlowKey, dir bool) string {
	if dir {
		return ">" + k.String()
	}
	return "<" + k.String()
}

// String 格式化五元组（key 用）。
// String formats the 5-tuple (for keys).
func (k FlowKey) String() string {
	return net.JoinHostPort(k.SrcIP.String(), strconv.Itoa(int(k.SrcPort))) + "-" +
		net.JoinHostPort(k.DstIP.String(), strconv.Itoa(int(k.DstPort))) + "/" + strconv.Itoa(int(k.Proto))
}

// FlowEvent 是一次流事件。
// FlowEvent is one flow event.
type FlowEvent struct {
	Flow *Flow
	Data []byte
	Op   Op
}

// Analyzer 接收流事件。
// Analyzer receives flow events.
type Analyzer interface {
	OnFlowEvent(ev FlowEvent)
}

// Sniffer 组装管线并驱动循环。
// Sniffer assembles the pipeline and drives the loop.
type Sniffer struct {
	src      Source
	tracker  *FlowTracker
	reassemb *Reassembler
	defrag   *Defragmenter
	analyzer Analyzer
}

// New 创建嗅探器。
// New creates a sniffer.
func New(src Source, a Analyzer) *Sniffer {
	return &Sniffer{
		src:      src,
		tracker:  NewFlowTracker(),
		reassemb: NewReassembler(),
		defrag:   NewDefragmenter(),
		analyzer: a,
	}
}

// Run 循环：Source → Parse → Track → 重组 → 回调，直至 EOF 或 ctx 取消。
// Run loops: Source → Parse → Track → reassemble → callbacks, until EOF or
// ctx cancellation.
func (s *Sniffer) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		pkt, err := s.src.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.process(pkt)
	}
}

// process 处理单个报文。
// process handles one packet.
func (s *Sniffer) process(pkt *packet.Packet) {
	if _, err := pkt.Parse(); err != nil {
		return
	}
	// IPv4 分片重组：重组完成后用重组载荷重解码传输层。
	if ip4, ok := pkt.NetworkLayer().(*layers.IPv4); ok && (ip4.FragOffset > 0 || ip4.Flags&0x01 != 0) {
		payload, done := s.defrag.Push(ip4)
		if !done {
			return
		}
		var lt layers.LayerType
		switch ip4.Protocol {
		case layers.ProtocolTCP:
			lt = layers.LayerTypeTCP
		case layers.ProtocolUDP:
			lt = layers.LayerTypeUDP
		default:
			return
		}
		tl, err := layers.DecodeLayer(lt, payload)
		if err != nil {
			return
		}
		key, ok := flowKeyOf(ip4, tl)
		if !ok {
			return
		}
		f, _ := s.tracker.trackKey(key, pkt.Timestamp)
		s.analyzer.OnFlowEvent(FlowEvent{Flow: f, Op: OpNew})
		if t, ok := tl.(*layers.TCP); ok {
			s.analyzer.OnFlowEvent(FlowEvent{Flow: f, Data: t.Payload(), Op: OpData})
		} else if u, ok := tl.(*layers.UDP); ok {
			s.analyzer.OnFlowEvent(FlowEvent{Flow: f, Data: u.Payload(), Op: OpData})
		}
		return
	}

	flow, isNew := s.tracker.Track(pkt)
	if flow == nil {
		return
	}
	if isNew {
		s.analyzer.OnFlowEvent(FlowEvent{Flow: flow, Op: OpNew})
	}
	tl := pkt.TransportLayer()
	switch t := tl.(type) {
	case *layers.TCP:
		segs := s.reassemb.Push(flow.Key, true, t.Seq, t.Payload())
		for _, seg := range segs {
			if len(seg) == 0 {
				continue // 纯 ACK/FIN 段不产生数据事件
			}
			s.analyzer.OnFlowEvent(FlowEvent{Flow: flow, Data: seg, Op: OpData})
		}
		if t.HasFlag(layers.TCPFlagFIN) || t.HasFlag(layers.TCPFlagRST) {
			s.reassemb.Close(flow.Key, true)
			s.analyzer.OnFlowEvent(FlowEvent{Flow: flow, Op: OpEnd})
		}
	case *layers.UDP:
		s.analyzer.OnFlowEvent(FlowEvent{Flow: flow, Data: t.Payload(), Op: OpData})
	}
}

// ---- 报文来源适配 ----

// SliceSource 从内存报文序列读取（测试与离线回放用）。
// SliceSource reads from an in-memory packet slice (tests and offline
// replay).
type SliceSource struct {
	pkts []*packet.Packet
	pos  int
}

// NewSliceSource 创建切片来源。
// NewSliceSource creates a slice source.
func NewSliceSource(pkts ...*packet.Packet) *SliceSource {
	return &SliceSource{pkts: pkts}
}

// Next 返回下一个报文；流尽返回 io.EOF。
// Next returns the next packet; io.EOF at the end.
func (s *SliceSource) Next() (*packet.Packet, error) {
	if s.pos >= len(s.pkts) {
		return nil, io.EOF
	}
	p := s.pkts[s.pos]
	s.pos++
	return p, nil
}

// PcapSource 读取 pcap 文件。
// PcapSource reads a pcap file.
type PcapSource struct {
	r *pcap.Reader
}

// NewPcapSource 打开 pcap 文件来源。
// NewPcapSource opens a pcap file source.
func NewPcapSource(r *pcap.Reader) *PcapSource { return &PcapSource{r: r} }

// Next 返回下一个报文。
// Next returns the next packet.
func (s *PcapSource) Next() (*packet.Packet, error) {
	pkt, err := s.r.ReadPacket()
	if err != nil {
		return nil, err
	}
	return packet.FromPCAP(pkt), nil
}

// PcapngSource 读取 pcapng 文件。
// PcapngSource reads a pcapng file.
type PcapngSource struct {
	r *pcapng.Reader
}

// NewPcapngSource 打开 pcapng 文件来源。
// NewPcapngSource opens a pcapng file source.
func NewPcapngSource(r *pcapng.Reader) *PcapngSource { return &PcapngSource{r: r} }

// Next 返回下一个报文。
// Next returns the next packet.
func (s *PcapngSource) Next() (*packet.Packet, error) {
	pkt, err := s.r.ReadPacket()
	if err != nil {
		return nil, err
	}
	return packet.FromPCAPNG(pkt), nil
}
