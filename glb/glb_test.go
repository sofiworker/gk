package glb

import (
	"context"
	"testing"
)

// MockDiscovery
type MockDiscovery struct {
	instances map[string][]Instance
}

func (m *MockDiscovery) GetInstances(serviceName string) ([]Instance, error) {
	return m.instances[serviceName], nil
}

func (m *MockDiscovery) Watch(serviceName string) (<-chan []Instance, error) {
	ch := make(chan []Instance)
	return ch, nil
}

func TestLoadBalancer(t *testing.T) {
	discovery := &MockDiscovery{
		instances: map[string][]Instance{
			"serviceA": {
				&BaseInstance{Address: "addr1", Healthy: true, Weight: 1},
				&BaseInstance{Address: "addr2", Healthy: true, Weight: 1},
				&BaseInstance{Address: "addr3", Healthy: false, Weight: 1}, // Unhealthy
			},
		},
	}
	
	strategy := NewRandomStrategy()
	lb := NewLoadBalancer(discovery, strategy)
	
	ctx := context.Background()
	
	// Test GetInstance
	inst, err := lb.GetInstance(ctx, "serviceA")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if inst.GetAddress() == "addr3" {
		t.Error("Got unhealthy instance")
	}
	
	// Test Refresh (implied by GetInstance first call)
	
	// Test No Instances
	_, err = lb.GetInstance(ctx, "serviceB")
	if err == nil {
		t.Error("Expected error for missing service")
	}
}

func TestStrategies(t *testing.T) {
	ctx := context.Background()
	instances := []Instance{
		&BaseInstance{Address: "a1", Healthy: true, Weight: 1},
		&BaseInstance{Address: "a2", Healthy: true, Weight: 2},
		&BaseInstance{Address: "a3", Healthy: true, Weight: 3},
	}

	// Random
	rs := NewRandomStrategy()
	if rs.Name() != "random" { t.Error("bad name") }
	for i := 0; i < 10; i++ {
		inst, err := rs.Next(ctx, instances)
		if err != nil { t.Fatal(err) }
		if inst == nil { t.Fatal("nil instance") }
	}

	// RoundRobin
	rrs := NewRoundRobinStrategy()
	if rrs.Name() != "round_robin" { t.Error("bad name") }
	// Expect a1, a2, a3, a1... since it indexes by instances[0].Address which is constant "a1" here?
	// Wait, RoundRobin uses "key := instances[0].GetAddress()".
	// If the list is passed every time, instances[0] is "a1". So key is "a1".
	// Map stores index for "a1".
	// So 0, 1, 2, 0...
	
	got1, _ := rrs.Next(ctx, instances)
	got2, _ := rrs.Next(ctx, instances)
	got3, _ := rrs.Next(ctx, instances)
	got4, _ := rrs.Next(ctx, instances)
	
	if got1.GetAddress() != "a1" { t.Error("rr 1 mismatch") }
	if got2.GetAddress() != "a2" { t.Error("rr 2 mismatch") }
	if got3.GetAddress() != "a3" { t.Error("rr 3 mismatch") }
	if got4.GetAddress() != "a1" { t.Error("rr 4 mismatch") }

	// WeightedRoundRobin
	wrr := NewWeightedRoundRobinStrategy()
	if wrr.Name() != "weighted_round_robin" { t.Error("bad name") }
	// Logic is complex stateful.
	// Just check it returns valid instances and doesn't crash.
	for i := 0; i < 20; i++ {
		inst, err := wrr.Next(ctx, instances)
		if err != nil { t.Fatal(err) }
		if inst == nil { t.Fatal("nil instance") }
	}

	// LeastConnections
	lcs := NewLeastConnectionsStrategy()
	if lcs.Name() != "least_connections" { t.Error("bad name") }
	
	// All 0 connections, should pick one (logic picks first min).
	// "minConnections == -1 || conns < minConnections"
	// 0 < -1 is false? No. min starts -1.
	// First: conns=0. 0 < -1 (false) OR min==-1 (true). Selected=a1. min=0.
	// Second: conns=0. 0 < 0 (false).
	// So it picks a1.
	
	inst, _ := lcs.Next(ctx, instances)
	if inst.GetAddress() != "a1" { t.Error("lc expect a1") }
	
	lcs.IncreaseConnections("a1")
	// Now a1=1. a2=0.
	inst, _ = lcs.Next(ctx, instances)
	if inst.GetAddress() != "a2" { t.Error("lc expect a2") }
	
	lcs.DecreaseConnections("a1")
	// a1=0.
	inst, _ = lcs.Next(ctx, instances)
	if inst.GetAddress() != "a1" { t.Error("lc expect a1 again") }
}

func TestEmptyInstances(t *testing.T) {
	ctx := context.Background()
	var instances []Instance
	
	s1 := NewRandomStrategy()
	if _, err := s1.Next(ctx, instances); err == nil { t.Error("expected error") }
	
	s2 := NewRoundRobinStrategy()
	if _, err := s2.Next(ctx, instances); err == nil { t.Error("expected error") }
	
	s3 := NewWeightedRoundRobinStrategy()
	if _, err := s3.Next(ctx, instances); err == nil { t.Error("expected error") }
	
	s4 := NewLeastConnectionsStrategy()
	if _, err := s4.Next(ctx, instances); err == nil { t.Error("expected error") }
}
