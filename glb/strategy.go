package glb

import (
	"context"
	"errors"
	"sync"
	"time"

	"golang.org/x/exp/rand"
)

type RandomStrategy struct {
	random *rand.Rand
}

func NewRandomStrategy() *RandomStrategy {
	return &RandomStrategy{
		random: rand.New(rand.NewSource(uint64(time.Now().UnixNano()))),
	}
}

func (s *RandomStrategy) Name() string {
	return "random"
}

func (s *RandomStrategy) Next(ctx context.Context, instances []Instance) (Instance, error) {
	if len(instances) == 0 {
		return nil, errors.New("no instances available")
	}
	return instances[s.random.Intn(len(instances))], nil
}

// 轮询策略
type RoundRobinStrategy struct {
	mu    sync.Mutex
	index map[string]int
}

func NewRoundRobinStrategy() *RoundRobinStrategy {
	return &RoundRobinStrategy{
		index: make(map[string]int),
	}
}

func (s *RoundRobinStrategy) Name() string {
	return "round_robin"
}

func (s *RoundRobinStrategy) Next(ctx context.Context, instances []Instance) (Instance, error) {
	if len(instances) == 0 {
		return nil, errors.New("no instances available")
	}

	// 使用服务地址作为标识
	key := instances[0].GetAddress() // 简化实现

	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.index[key]
	instance := instances[index%len(instances)]
	s.index[key] = (index + 1) % len(instances)

	return instance, nil
}

// 加权轮询
type WeightedRoundRobinStrategy struct {
	mu             sync.Mutex
	currentWeights map[string]int
}

func NewWeightedRoundRobinStrategy() *WeightedRoundRobinStrategy {
	return &WeightedRoundRobinStrategy{
		currentWeights: make(map[string]int),
	}
}

func (s *WeightedRoundRobinStrategy) Name() string {
	return "weighted_round_robin"
}

func (s *WeightedRoundRobinStrategy) Next(ctx context.Context, instances []Instance) (Instance, error) {
	if len(instances) == 0 {
		return nil, errors.New("no instances available")
	}

	key := instances[0].GetAddress() // 简化实现

	s.mu.Lock()
	defer s.mu.Unlock()

	totalWeight := 0
	for _, instance := range instances {
		totalWeight += instance.GetWeight()
	}

	if s.currentWeights[key] >= totalWeight {
		s.currentWeights[key] = 0
	}

	current := s.currentWeights[key]
	var selected Instance

	for _, instance := range instances {
		if current < instance.GetWeight() {
			selected = instance
			break
		}
		current -= instance.GetWeight()
	}

	s.currentWeights[key]++
	return selected, nil
}

// 最少连接策略
type LeastConnectionsStrategy struct {
	mu          sync.RWMutex
	connections map[string]int64
}

func NewLeastConnectionsStrategy() *LeastConnectionsStrategy {
	return &LeastConnectionsStrategy{
		connections: make(map[string]int64),
	}
}

func (s *LeastConnectionsStrategy) Name() string {
	return "least_connections"
}

func (s *LeastConnectionsStrategy) Next(ctx context.Context, instances []Instance) (Instance, error) {
	if len(instances) == 0 {
		return nil, errors.New("no instances available")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var selected Instance
	var minConnections int64 = -1

	for _, instance := range instances {
		conns := s.connections[instance.GetAddress()]
		if minConnections == -1 || conns < minConnections {
			minConnections = conns
			selected = instance
		}
	}

	return selected, nil
}

func (s *LeastConnectionsStrategy) IncreaseConnections(address string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connections[address]++
}

func (s *LeastConnectionsStrategy) DecreaseConnections(address string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connections[address] > 0 {
		s.connections[address]--
	}
}
