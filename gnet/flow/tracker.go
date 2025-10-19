package flow

import (
	"net"
	"sync"
	"time"

	"github.com/sofiworker/gk/gnet/layers"
)

type FlowKey string

type Flow struct {
	SrcIP, DstIP     net.IP
	SrcPort, DstPort uint16
	Protocol         layers.LayerType
}

type FlowState struct {
	StartTime   time.Time
	EndTime     time.Time
	BytesSent   uint64
	BytesRecv   uint64
	PacketsSent uint32
	PacketsRecv uint32
}

// 连接跟踪器
type Tracker struct {
	flows   map[FlowKey]*FlowState
	mutex   sync.RWMutex
	timeout time.Duration
}

func NewTracker() *Tracker {
	return &Tracker{
		flows: make(map[FlowKey]*FlowState),
	}
}

func (t *Tracker) ProcessPacket(packet *layers.IPv4, tcp *layers.TCP) {
	key := FlowKey("")

	t.mutex.Lock()
	defer t.mutex.Unlock()

	if state, exists := t.flows[key]; exists {
		state.PacketsRecv++
		state.BytesRecv += uint64(len(tcp.Payload()))
		state.EndTime = time.Now()
	} else {
		t.flows[key] = &FlowState{
			StartTime:   time.Now(),
			PacketsRecv: 1,
			BytesRecv:   uint64(len(tcp.Payload())),
		}
	}
}

// 定期清理过期连接
func (t *Tracker) Cleanup() {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	now := time.Now()
	for key, state := range t.flows {
		if now.Sub(state.EndTime) > t.timeout {
			delete(t.flows, key)
		}
	}
}
