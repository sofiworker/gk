package gsd

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrNoAvailableInstances = errors.New("gsd: no available instances")
	ErrNilDiscovery         = errors.New("gsd: discovery is nil")
	ErrNilStrategy          = errors.New("gsd: strategy is nil")
)

type Instance interface {
	GetAddress() string
	IsHealthy() bool
	GetWeight() int
	GetMetadata() map[string]string
}

type Strategy interface {
	Name() string
	Next(context.Context, []Instance) (Instance, error)
}

type Discovery interface {
	GetInstances(serviceName string) ([]Instance, error)
	Watch(serviceName string) (<-chan []Instance, error)
}

type BaseInstance struct {
	Address  string
	Healthy  bool
	Weight   int
	Metadata map[string]string
}

func (i *BaseInstance) GetAddress() string {
	return i.Address
}

func (i *BaseInstance) IsHealthy() bool {
	return i.Healthy
}

func (i *BaseInstance) GetWeight() int {
	return i.Weight
}

func (i *BaseInstance) GetMetadata() map[string]string {
	return i.Metadata
}

type DiscoveryLoadBalancer struct {
	discovery Discovery
	strategy  Strategy
	mu        sync.RWMutex
	instances map[string][]Instance
}

func NewLoadBalancer(discovery Discovery, strategy Strategy) *DiscoveryLoadBalancer {
	return &DiscoveryLoadBalancer{
		discovery: discovery,
		strategy:  strategy,
		instances: make(map[string][]Instance),
	}
}

func (lb *DiscoveryLoadBalancer) GetInstance(ctx context.Context, serviceName string) (Instance, error) {
	if lb.discovery == nil {
		return nil, ErrNilDiscovery
	}
	if lb.strategy == nil {
		return nil, ErrNilStrategy
	}

	instances, err := lb.getHealthyInstances(serviceName)
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return nil, ErrNoAvailableInstances
	}

	return lb.strategy.Next(ctx, instances)
}

func (lb *DiscoveryLoadBalancer) getHealthyInstances(serviceName string) ([]Instance, error) {
	lb.mu.RLock()
	instances, exists := lb.instances[serviceName]
	lb.mu.RUnlock()

	if !exists {
		if err := lb.refreshInstances(serviceName); err != nil {
			return nil, err
		}
		lb.mu.RLock()
		instances = lb.instances[serviceName]
		lb.mu.RUnlock()
	}

	healthy := make([]Instance, 0, len(instances))
	for _, instance := range instances {
		if instance.IsHealthy() {
			healthy = append(healthy, instance)
		}
	}

	return healthy, nil
}

func (lb *DiscoveryLoadBalancer) refreshInstances(serviceName string) error {
	instances, err := lb.discovery.GetInstances(serviceName)
	if err != nil {
		return err
	}

	lb.mu.Lock()
	lb.instances[serviceName] = instances
	lb.mu.Unlock()

	return nil
}

func (lb *DiscoveryLoadBalancer) StartWatching(serviceName string) error {
	if lb.discovery == nil {
		return ErrNilDiscovery
	}

	ch, err := lb.discovery.Watch(serviceName)
	if err != nil {
		return err
	}

	go func() {
		for instances := range ch {
			lb.mu.Lock()
			lb.instances[serviceName] = instances
			lb.mu.Unlock()
		}
	}()

	return nil
}
