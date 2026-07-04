package gsd

import (
	"context"
	"testing"
)

type mockDiscovery struct {
	instances map[string][]Instance
	watch     chan []Instance
}

func (m *mockDiscovery) GetInstances(serviceName string) ([]Instance, error) {
	return m.instances[serviceName], nil
}

func (m *mockDiscovery) Watch(serviceName string) (<-chan []Instance, error) {
	return m.watch, nil
}

func TestDiscoveryLoadBalancerFiltersUnhealthyInstances(t *testing.T) {
	discovery := &mockDiscovery{
		instances: map[string][]Instance{
			"api": {
				&BaseInstance{Address: "10.0.0.1", Healthy: true, Weight: 1},
				&BaseInstance{Address: "10.0.0.2", Healthy: false, Weight: 1},
			},
		},
		watch: make(chan []Instance),
	}

	lb := NewLoadBalancer(discovery, NewRoundRobinStrategy())

	got, err := lb.GetInstance(context.Background(), "api")
	if err != nil {
		t.Fatalf("GetInstance returned error: %v", err)
	}
	if got.GetAddress() != "10.0.0.1" {
		t.Fatalf("GetInstance selected unhealthy instance %q", got.GetAddress())
	}
}

func TestDiscoveryLoadBalancerReportsMissingHealthyInstances(t *testing.T) {
	discovery := &mockDiscovery{
		instances: map[string][]Instance{
			"api": {
				&BaseInstance{Address: "10.0.0.1", Healthy: false, Weight: 1},
			},
		},
		watch: make(chan []Instance),
	}

	lb := NewLoadBalancer(discovery, NewRoundRobinStrategy())

	_, err := lb.GetInstance(context.Background(), "api")
	if err == nil {
		t.Fatal("GetInstance expected error for service without healthy instances")
	}
}

func TestInstanceStrategies(t *testing.T) {
	ctx := context.Background()
	instances := []Instance{
		&BaseInstance{Address: "a1", Healthy: true, Weight: 1},
		&BaseInstance{Address: "a2", Healthy: true, Weight: 2},
		&BaseInstance{Address: "a3", Healthy: true, Weight: 3},
	}

	rr := NewRoundRobinStrategy()
	first, err := rr.Next(ctx, instances)
	if err != nil {
		t.Fatalf("RoundRobinStrategy first selection: %v", err)
	}
	second, err := rr.Next(ctx, instances)
	if err != nil {
		t.Fatalf("RoundRobinStrategy second selection: %v", err)
	}
	third, err := rr.Next(ctx, instances)
	if err != nil {
		t.Fatalf("RoundRobinStrategy third selection: %v", err)
	}
	if first.GetAddress() != "a1" || second.GetAddress() != "a2" || third.GetAddress() != "a3" {
		t.Fatalf("RoundRobinStrategy selected %q, %q, %q", first.GetAddress(), second.GetAddress(), third.GetAddress())
	}

	least := NewLeastConnectionsStrategy()
	least.IncreaseConnections("a1")
	got, err := least.Next(ctx, instances)
	if err != nil {
		t.Fatalf("LeastConnectionsStrategy selection: %v", err)
	}
	if got.GetAddress() != "a2" {
		t.Fatalf("LeastConnectionsStrategy selected %q, want a2", got.GetAddress())
	}
}
