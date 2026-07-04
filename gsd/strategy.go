package gsd

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

type RandomStrategy struct {
	mu     sync.Mutex
	random *rand.Rand
}

func NewRandomStrategy() *RandomStrategy {
	return &RandomStrategy{
		random: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *RandomStrategy) Name() string {
	return "random"
}

func (s *RandomStrategy) Next(ctx context.Context, instances []Instance) (Instance, error) {
	if len(instances) == 0 {
		return nil, ErrNoAvailableInstances
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return instances[s.random.Intn(len(instances))], nil
}

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
		return nil, ErrNoAvailableInstances
	}

	key := instanceSetKey(instances)
	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.index[key]
	instance := instances[index%len(instances)]
	s.index[key] = (index + 1) % len(instances)

	return instance, nil
}

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
		return nil, ErrNoAvailableInstances
	}

	key := instanceSetKey(instances)
	s.mu.Lock()
	defer s.mu.Unlock()

	totalWeight := 0
	for _, instance := range instances {
		weight := instance.GetWeight()
		if weight > 0 {
			totalWeight += weight
		}
	}
	if totalWeight <= 0 {
		return instances[0], nil
	}

	if s.currentWeights[key] >= totalWeight {
		s.currentWeights[key] = 0
	}

	current := s.currentWeights[key]
	var selected Instance
	for _, instance := range instances {
		weight := instance.GetWeight()
		if weight <= 0 {
			continue
		}
		if current < weight {
			selected = instance
			break
		}
		current -= weight
	}

	s.currentWeights[key]++
	if selected == nil {
		selected = instances[0]
	}
	return selected, nil
}

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
		return nil, ErrNoAvailableInstances
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var selected Instance
	var minConnections int64 = -1
	for _, instance := range instances {
		connections := s.connections[instance.GetAddress()]
		if minConnections == -1 || connections < minConnections {
			minConnections = connections
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

func instanceSetKey(instances []Instance) string {
	if len(instances) == 0 {
		return ""
	}
	return instances[0].GetAddress()
}
