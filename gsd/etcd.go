package gsd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdRegistry etcd服务注册实现
type EtcdRegistry struct {
	client       *clientv3.Client
	ctx          context.Context
	cancel       context.CancelFunc
	config       RegistryConfig
	keyFormatter KeyFormatter
}

// NewEtcdRegistry 创建etcd服务注册实例
func NewEtcdRegistry(config RegistryConfig, keyFormatter KeyFormatter) (*EtcdRegistry, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   config.Endpoints,
		DialTimeout: config.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to etcd: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	if keyFormatter == nil {
		keyFormatter = NewDefaultKeyFormatter("")
	}

	return &EtcdRegistry{
		client:       cli,
		ctx:          ctx,
		cancel:       cancel,
		config:       config,
		keyFormatter: keyFormatter,
	}, nil
}

// Register 服务注册
func (e *EtcdRegistry) Register(serviceInfo ServiceInfo) error {
	// 设置时间戳和状态
	serviceInfo.Timestamp = time.Now().Unix()
	if serviceInfo.Status == "" {
		serviceInfo.Status = ServiceStatusHealthy
	}

	key := e.keyFormatter.Format(serviceInfo)
	value, err := json.Marshal(serviceInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal service info: %w", err)
	}

	// 创建租约
	lease := clientv3.NewLease(e.client)

	registerFunc := func() error {
		leaseResp, err := lease.Grant(e.ctx, e.config.LeaseTTL)
		if err != nil {
			return fmt.Errorf("failed to create lease: %w", err)
		}

		// 使用租约设置键值对
		_, err = e.client.Put(e.ctx, key, string(value), clientv3.WithLease(leaseResp.ID))
		if err != nil {
			return fmt.Errorf("failed to put service info: %w", err)
		}
		return nil
	}

	err = retryWithBackoff(registerFunc, e.config.ErrorHandlingOptions)
	if err != nil {
		return fmt.Errorf("failed to register service after retries: %w", err)
	}

	// 设置定时续租
	go e.keepAlive(lease, key, value)

	return nil
}

// keepAlive 续租函数
func (e *EtcdRegistry) keepAlive(lease clientv3.Lease, key string, value []byte) {
	keepAliveFunc := func() (<-chan *clientv3.LeaseKeepAliveResponse, error) {
		// 从key中提取租约ID，这里简化处理，实际需要从etcd获取
		// 为简化实现，我们重新注册服务
		var serviceInfo ServiceInfo
		err := json.Unmarshal(value, &serviceInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal service info: %w", err)
		}

		// 获取租约ID（实际应用中需要从etcd获取）
		// 这里我们重新创建租约并注册
		leaseResp, err := lease.Grant(e.ctx, e.config.LeaseTTL)
		if err != nil {
			return nil, fmt.Errorf("failed to create lease: %w", err)
		}

		_, err = e.client.Put(e.ctx, key, string(value), clientv3.WithLease(leaseResp.ID))
		if err != nil {
			return nil, fmt.Errorf("failed to put service info: %w", err)
		}

		return lease.KeepAlive(e.ctx, leaseResp.ID)
	}

	// 尝试建立keep alive连接
	keepAlive, err := keepAliveFunc()
	if err != nil {
		log.Printf("Failed to establish keep alive for key %s: %v", key, err)
		return
	}

	for {
		select {
		case _, ok := <-keepAlive:
			if !ok {
				// 续租通道关闭，尝试重新建立
				log.Printf("Keep alive channel closed for key %s, re-establishing...", key)
				time.Sleep(1 * time.Second) // 避免过于频繁的重试

				newKeepAlive, err := keepAliveFunc()
				if err != nil {
					log.Printf("Failed to re-establish keep alive for key %s: %v", key, err)
					// 等待后继续尝试
					time.Sleep(5 * time.Second)
					continue
				}
				keepAlive = newKeepAlive
			}
		case <-e.ctx.Done():
			return
		}
	}
}

// Unregister 服务注销
func (e *EtcdRegistry) Unregister(serviceInfo ServiceInfo) error {
	key := e.keyFormatter.Format(serviceInfo)

	unregisterFunc := func() error {
		_, err := e.client.Delete(e.ctx, key)
		if err != nil {
			return fmt.Errorf("failed to delete service key %s: %w", key, err)
		}
		return nil
	}

	err := retryWithBackoff(unregisterFunc, e.config.ErrorHandlingOptions)
	if err != nil {
		return fmt.Errorf("failed to unregister service after retries: %w", err)
	}

	return nil
}

// Discover 服务发现
func (e *EtcdRegistry) Discover(serviceName string) ([]ServiceInfo, error) {
	prefix := fmt.Sprintf("%s/%s/", e.keyFormatter.(*DefaultKeyFormatter).Prefix, serviceName)

	var services []ServiceInfo
	discoverFunc := func() error {
		resp, err := e.client.Get(e.ctx, prefix, clientv3.WithPrefix())
		if err != nil {
			return fmt.Errorf("failed to get services with prefix %s: %w", prefix, err)
		}

		services = nil // 重置切片
		for _, kv := range resp.Kvs {
			var serviceInfo ServiceInfo
			err := json.Unmarshal(kv.Value, &serviceInfo)
			if err != nil {
				log.Printf("Failed to unmarshal service info: %v", err)
				continue
			}
			services = append(services, serviceInfo)
		}

		return nil
	}

	err := retryWithBackoff(discoverFunc, e.config.ErrorHandlingOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to discover services after retries: %w", err)
	}

	return services, nil
}

// Watch 服务监听
func (e *EtcdRegistry) Watch(serviceName string, handler func([]ServiceInfo)) error {
	prefix := fmt.Sprintf("%s/%s/", e.keyFormatter.(*DefaultKeyFormatter).Prefix, serviceName)

	go func() {
		// 初始化时先获取一次服务列表
		initialServices, err := e.Discover(serviceName)
		if err == nil && handler != nil {
			handler(initialServices)
		}

		// 建立watch连接
		for {
			watchChan := e.client.Watch(e.ctx, prefix, clientv3.WithPrefix())

			for watchResp := range watchChan {
				if watchResp.Canceled {
					log.Printf("Watch canceled for service %s, attempting to re-establish", serviceName)
					// 短暂等待后重新建立watch
					time.Sleep(1 * time.Second)
					break // 跳出内层循环，重新建立watch
				}

				for _, event := range watchResp.Events {
					log.Printf("Watch event: %s %s", event.Type, event.Kv.Key)
				}

				// 获取当前所有服务实例并通知处理器
				services, err := e.Discover(serviceName)
				if err != nil {
					log.Printf("Failed to discover services in watch: %v", err)
					continue
				}
				if handler != nil {
					handler(services)
				}
			}

			// 如果watch被取消，等待一段时间后重试
			select {
			case <-e.ctx.Done():
				return
			default:
				time.Sleep(2 * time.Second)
			}
		}
	}()

	return nil
}

// Close 关闭连接
func (e *EtcdRegistry) Close() error {
	e.cancel()
	return e.client.Close()
}
