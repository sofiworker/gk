package gsd

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// ServiceInfo 服务信息结构体
type ServiceInfo struct {
	Name      string            `json:"name"`
	Address   string            `json:"address"`
	Port      int               `json:"port"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp int64             `json:"timestamp"`
	Status    ServiceStatus     `json:"status"`
}

// ServiceStatus 服务状态
type ServiceStatus string

const (
	ServiceStatusHealthy   ServiceStatus = "healthy"
	ServiceStatusUnhealthy ServiceStatus = "unhealthy"
	ServiceStatusUnknown   ServiceStatus = "unknown"
)

// ErrorHandlingOptions 错误处理选项
type ErrorHandlingOptions struct {
	MaxRetries          int              `json:"max_retries"`
	RetryDelay          time.Duration    `json:"retry_delay"`
	MaxRetryDelay       time.Duration    `json:"max_retry_delay"`
	RetryStrategy       RetryStrategy    `json:"retry_strategy"`
	BackoffMultiplier   float64          `json:"backoff_multiplier"`
	TransientErrors     []string         `json:"transient_errors"`
	ShouldRetry         func(error) bool `json:"-"`
	Timeout             time.Duration    `json:"timeout"`
	HealthCheckInterval time.Duration    `json:"health_check_interval"`
}

// DefaultErrorHandlingOptions 默认错误处理选项
var DefaultErrorHandlingOptions = ErrorHandlingOptions{
	MaxRetries:          3,
	RetryDelay:          1 * time.Second,
	MaxRetryDelay:       30 * time.Second,
	RetryStrategy:       RetryStrategyExponential,
	BackoffMultiplier:   2.0,
	TransientErrors:     []string{"connection refused", "timeout", "unavailable", "etcdserver: leader changed"},
	Timeout:             10 * time.Second,
	HealthCheckInterval: 30 * time.Second,
	ShouldRetry: func(err error) bool {
		if err == nil {
			return false
		}
		return false
	},
}

// RegistryConfig 注册中心配置
type RegistryConfig struct {
	Endpoints            []string
	DialTimeout          time.Duration
	LeaseTTL             int64
	ErrorHandlingOptions ErrorHandlingOptions
}

// DefaultRegistryConfig 默认配置
var DefaultRegistryConfig = RegistryConfig{
	Endpoints:            []string{"localhost:2379"},
	DialTimeout:          5 * time.Second,
	LeaseTTL:             10,
	ErrorHandlingOptions: DefaultErrorHandlingOptions,
}

// KeyFormatter 键格式化器接口
type KeyFormatter interface {
	Format(serviceInfo ServiceInfo) string
	Parse(key string) (ServiceInfo, error)
}

// DefaultKeyFormatter 默认键格式化器
type DefaultKeyFormatter struct {
	Prefix string
}

func NewDefaultKeyFormatter(prefix string) *DefaultKeyFormatter {
	if prefix == "" {
		prefix = "/services"
	}
	return &DefaultKeyFormatter{Prefix: prefix}
}

func (f *DefaultKeyFormatter) Format(serviceInfo ServiceInfo) string {
	return fmt.Sprintf("%s/%s/%s:%d", f.Prefix, serviceInfo.Name, serviceInfo.Address, serviceInfo.Port)
}

func (f *DefaultKeyFormatter) Parse(key string) (ServiceInfo, error) {
	// 简化实现，实际应用中需要更复杂的解析逻辑
	var serviceInfo ServiceInfo
	return serviceInfo, nil
}

// ServiceRegistry 服务注册接口
type ServiceRegistry interface {
	Register(serviceInfo ServiceInfo) error
	Unregister(serviceInfo ServiceInfo) error
	Discover(serviceName string) ([]ServiceInfo, error)
	Watch(serviceName string, handler func([]ServiceInfo)) error
	Close() error
}

// RegistryFactory 注册中心工厂接口
type RegistryFactory interface {
	Create(config interface{}) (ServiceRegistry, error)
}

// LoadBalancer 负载均衡器接口
type LoadBalancer interface {
	Select(services []ServiceInfo) *ServiceInfo
}

// RandomLoadBalancer 随机负载均衡器
type RandomLoadBalancer struct {
	rand *rand.Rand
}

func NewRandomLoadBalancer() *RandomLoadBalancer {
	return &RandomLoadBalancer{
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (r *RandomLoadBalancer) Select(services []ServiceInfo) *ServiceInfo {
	if len(services) == 0 {
		return nil
	}
	return &services[r.rand.Intn(len(services))]
}

// RoundRobinLoadBalancer 轮询负载均衡器
type RoundRobinLoadBalancer struct {
	current int
	mutex   sync.Mutex
}

func NewRoundRobinLoadBalancer() *RoundRobinLoadBalancer {
	return &RoundRobinLoadBalancer{
		current: 0,
	}
}

func (r *RoundRobinLoadBalancer) Select(services []ServiceInfo) *ServiceInfo {
	if len(services) == 0 {
		return nil
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	service := &services[r.current%len(services)]
	r.current++
	return service
}

// HealthChecker 健康检查接口
type HealthChecker interface {
	Check(serviceInfo ServiceInfo) bool
}

// HTTPHealthChecker HTTP健康检查器
type HTTPHealthChecker struct {
	Timeout time.Duration
	Client  *http.Client
	Options ErrorHandlingOptions
}

func NewHTTPHealthChecker(timeout time.Duration, options ErrorHandlingOptions) *HTTPHealthChecker {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	client := &http.Client{
		Timeout: timeout,
	}

	if options.Timeout > 0 {
		client.Timeout = options.Timeout
	}

	return &HTTPHealthChecker{
		Timeout: client.Timeout,
		Client:  client,
		Options: options,
	}
}

func (h *HTTPHealthChecker) Check(serviceInfo ServiceInfo) bool {
	healthURL := fmt.Sprintf("http://%s:%d/health", serviceInfo.Address, serviceInfo.Port)

	var success bool
	checkFunc := func() error {
		resp, err := h.Client.Get(healthURL)
		if err != nil {
			log.Printf("Health check failed for %s:%d: %v", serviceInfo.Address, serviceInfo.Port, err)
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return fmt.Errorf("health check returned status code %d", resp.StatusCode)
		}

		success = true
		return nil
	}

	err := retryWithBackoff(checkFunc, h.Options)
	if err != nil {
		log.Printf("Health check failed after retries for %s:%d: %v", serviceInfo.Address, serviceInfo.Port, err)
		return false
	}

	return success
}

// FilterHealthyServices 过滤健康的服务
func FilterHealthyServices(services []ServiceInfo, checker HealthChecker) []ServiceInfo {
	var healthyServices []ServiceInfo
	for _, service := range services {
		if checker.Check(service) {
			healthyServices = append(healthyServices, service)
		}
	}
	return healthyServices
}

// ServiceDiscovery 服务发现接口
type ServiceDiscovery interface {
	GetService(serviceName string) ([]ServiceInfo, error)
	SelectService(serviceName string) (*ServiceInfo, error)
	WatchService(serviceName string, handler func([]ServiceInfo)) error
	Close() error
}

// ServiceRegistrar 服务注册接口
type ServiceRegistrar interface {
	Register(serviceInfo ServiceInfo) error
	Unregister(serviceInfo ServiceInfo) error
	Close() error
}

// ServiceClient 服务客户端（分离注册和发现）
type ServiceClient struct {
	registry  ServiceRegistrar
	discovery ServiceDiscovery
	options   ErrorHandlingOptions
}

// NewServiceClient 创建服务客户端
func NewServiceClient(registry ServiceRegistrar, discovery ServiceDiscovery, options ErrorHandlingOptions) *ServiceClient {
	if discovery == nil {
		discovery = registry.(ServiceDiscovery) // 如果没有提供独立的发现器，则使用注册器
	}

	return &ServiceClient{
		registry:  registry,
		discovery: discovery,
		options:   options,
	}
}

// Register 注册服务
func (c *ServiceClient) Register(serviceInfo ServiceInfo) error {
	registerFunc := func() error {
		return c.registry.Register(serviceInfo)
	}

	return retryWithBackoff(registerFunc, c.options)
}

// Unregister 注销服务
func (c *ServiceClient) Unregister(serviceInfo ServiceInfo) error {
	unregisterFunc := func() error {
		return c.registry.Unregister(serviceInfo)
	}

	return retryWithBackoff(unregisterFunc, c.options)
}

// GetService 获取服务实例
func (c *ServiceClient) GetService(serviceName string) ([]ServiceInfo, error) {
	var services []ServiceInfo
	getFunc := func() error {
		var err error
		services, err = c.discovery.GetService(serviceName)
		return err
	}

	err := retryWithBackoff(getFunc, c.options)
	return services, err
}

// SelectService 随机选择一个服务实例
func (c *ServiceClient) SelectService(serviceName string, loadBalancer LoadBalancer) (*ServiceInfo, error) {
	services, err := c.GetService(serviceName)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no available service instances for %s", serviceName)
	}

	if loadBalancer == nil {
		loadBalancer = NewRoundRobinLoadBalancer()
	}

	selected := loadBalancer.Select(services)
	return selected, nil
}

// WatchService 监听服务变化
func (c *ServiceClient) WatchService(serviceName string, handler func([]ServiceInfo)) error {
	return c.discovery.WatchService(serviceName, handler)
}

// Close 关闭客户端
func (c *ServiceClient) Close() error {
	var registryErr, discoveryErr error

	if c.registry != nil {
		registryErr = c.registry.Close()
	}

	if c.discovery != nil {
		discoveryErr = c.discovery.Close()
	}

	if registryErr != nil {
		return registryErr
	}
	return discoveryErr
}

// EtcdRegistryFactory etcd注册中心工厂
type EtcdRegistryFactory struct{}

func (f *EtcdRegistryFactory) Create(config interface{}) (ServiceRegistry, error) {
	cfg, ok := config.(RegistryConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type, expected RegistryConfig")
	}

	return NewEtcdRegistry(cfg, nil)
}

// CustomServiceInfo 自定义服务信息接口
type CustomServiceInfo interface {
	GetName() string
	GetAddress() string
	GetPort() int
	GetMetadata() map[string]string
	GetStatus() ServiceStatus
	SetStatus(status ServiceStatus)
}

// 实现默认的ServiceInfo为CustomServiceInfo接口
func (s ServiceInfo) GetName() string {
	return s.Name
}

func (s ServiceInfo) GetAddress() string {
	return s.Address
}

func (s ServiceInfo) GetPort() int {
	return s.Port
}

func (s ServiceInfo) GetMetadata() map[string]string {
	return s.Metadata
}

func (s ServiceInfo) GetStatus() ServiceStatus {
	return s.Status
}

func (s *ServiceInfo) SetStatus(status ServiceStatus) {
	s.Status = status
}

// ServiceManager 服务管理器
type ServiceManager struct {
	client   *ServiceClient
	checker  HealthChecker
	lb       LoadBalancer
	mutex    sync.RWMutex
	services map[string][]ServiceInfo
	options  ErrorHandlingOptions
}

// NewServiceManager 创建服务管理器
func NewServiceManager(client *ServiceClient, checker HealthChecker, lb LoadBalancer, options ErrorHandlingOptions) *ServiceManager {
	manager := &ServiceManager{
		client:   client,
		checker:  checker,
		lb:       lb,
		options:  options,
		services: make(map[string][]ServiceInfo),
	}

	return manager
}

// Register 注册服务
func (sm *ServiceManager) Register(serviceInfo ServiceInfo) error {
	return sm.client.Register(serviceInfo)
}

// Unregister 注销服务
func (sm *ServiceManager) Unregister(serviceInfo ServiceInfo) error {
	return sm.client.Unregister(serviceInfo)
}

// DiscoverAndCache 发现并缓存服务
func (sm *ServiceManager) DiscoverAndCache(serviceName string) error {
	var services []ServiceInfo
	discoverFunc := func() error {
		var err error
		services, err = sm.client.GetService(serviceName)
		return err
	}

	err := retryWithBackoff(discoverFunc, sm.options)
	if err != nil {
		return err
	}

	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	sm.services[serviceName] = services

	return nil
}

// GetCachedService 获取缓存的服务
func (sm *ServiceManager) GetCachedService(serviceName string) ([]ServiceInfo, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	services, exists := sm.services[serviceName]
	return services, exists
}

// SelectServiceWithHealthCheck 带健康检查的服务选择
func (sm *ServiceManager) SelectServiceWithHealthCheck(serviceName string) (*ServiceInfo, error) {
	services, exists := sm.GetCachedService(serviceName)
	if !exists {
		err := sm.DiscoverAndCache(serviceName)
		if err != nil {
			return nil, err
		}
		services, _ = sm.GetCachedService(serviceName)
	}

	if sm.checker != nil {
		services = FilterHealthyServices(services, sm.checker)
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("no healthy service instances for %s", serviceName)
	}

	if sm.lb == nil {
		sm.lb = NewRoundRobinLoadBalancer()
	}

	selected := sm.lb.Select(services)
	return selected, nil
}

// Close 关闭服务管理器
func (sm *ServiceManager) Close() error {
	return sm.client.Close()
}

type CustomService struct {
	Name      string
	Address   string
	Port      int
	Version   string
	Weight    int
	Timestamp int64
	Status    ServiceStatus
}

func (c CustomService) GetName() string {
	return c.Name
}

func (c CustomService) GetAddress() string {
	return c.Address
}

func (c CustomService) GetPort() int {
	return c.Port
}

func (c CustomService) GetMetadata() map[string]string {
	return map[string]string{
		"version": c.Version,
		"weight":  fmt.Sprintf("%d", c.Weight),
	}
}

func (c CustomService) GetStatus() ServiceStatus {
	return c.Status
}

func (c *CustomService) SetStatus(status ServiceStatus) {
	c.Status = status
}

// ToServiceInfo 转换为标准ServiceInfo
func (c CustomService) ToServiceInfo() ServiceInfo {
	return ServiceInfo{
		Name:      c.Name,
		Address:   c.Address,
		Port:      c.Port,
		Metadata:  c.GetMetadata(),
		Timestamp: c.Timestamp,
		Status:    c.GetStatus(),
	}
}

// WithRetryOption 应用重试选项的辅助函数
func WithRetryOption(options ErrorHandlingOptions) func(*ErrorHandlingOptions) {
	return func(opts *ErrorHandlingOptions) {
		*opts = options
	}
}

// WithMaxRetries 设置最大重试次数
func WithMaxRetries(maxRetries int) func(*ErrorHandlingOptions) {
	return func(opts *ErrorHandlingOptions) {
		opts.MaxRetries = maxRetries
	}
}

// WithRetryDelay 设置重试延迟
func WithRetryDelay(delay time.Duration) func(*ErrorHandlingOptions) {
	return func(opts *ErrorHandlingOptions) {
		opts.RetryDelay = delay
	}
}

// WithRetryStrategy 设置重试策略
func WithRetryStrategy(strategy RetryStrategy) func(*ErrorHandlingOptions) {
	return func(opts *ErrorHandlingOptions) {
		opts.RetryStrategy = strategy
	}
}

// BuildErrorHandlingOptions 构建错误处理选项
func BuildErrorHandlingOptions(modifiers ...func(*ErrorHandlingOptions)) ErrorHandlingOptions {
	options := DefaultErrorHandlingOptions
	for _, modifier := range modifiers {
		modifier(&options)
	}
	return options
}
