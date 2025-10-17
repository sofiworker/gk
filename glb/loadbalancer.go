package glb

import (
	"context"
	"errors"
	"sync"
)

// 服务实例
type Instance interface {
	GetAddress() string
	IsHealthy() bool
	GetWeight() int
	GetMetadata() map[string]string
}

// 负载均衡策略
type Strategy interface {
	Name() string
	Next(context.Context, []Instance) (Instance, error)
}

// 服务发现
type Discovery interface {
	GetInstances(serviceName string) ([]Instance, error)
	Watch(serviceName string) (<-chan []Instance, error)
}

// 基础服务实例
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

// 负载均衡器
type LoadBalancer struct {
	discovery Discovery
	strategy  Strategy
	mu        sync.RWMutex
	instances map[string][]Instance
}

func NewLoadBalancer(discovery Discovery, strategy Strategy) *LoadBalancer {
	return &LoadBalancer{
		discovery: discovery,
		strategy:  strategy,
		instances: make(map[string][]Instance),
	}
}

func (lb *LoadBalancer) GetInstance(ctx context.Context, serviceName string) (Instance, error) {
	instances, err := lb.getHealthyInstances(serviceName)
	if err != nil {
		return nil, err
	}

	if len(instances) == 0 {
		return nil, errors.New("no available instances")
	}

	return lb.strategy.Next(ctx, instances)
}

func (lb *LoadBalancer) getHealthyInstances(serviceName string) ([]Instance, error) {
	lb.mu.RLock()
	instances, exists := lb.instances[serviceName]
	lb.mu.RUnlock()

	if !exists {
		if err := lb.refreshInstances(serviceName); err != nil {
			return nil, err
		}
	}

	// 过滤健康实例
	var healthy []Instance
	for _, instance := range instances {
		if instance.IsHealthy() {
			healthy = append(healthy, instance)
		}
	}

	return healthy, nil
}

func (lb *LoadBalancer) refreshInstances(serviceName string) error {
	instances, err := lb.discovery.GetInstances(serviceName)
	if err != nil {
		return err
	}

	lb.mu.Lock()
	lb.instances[serviceName] = instances
	lb.mu.Unlock()

	return nil
}

// 启动监听
func (lb *LoadBalancer) StartWatching(serviceName string) error {
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
