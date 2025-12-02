package gconfig

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
	_ "github.com/spf13/viper/remote" // 匿名导入以支持远程配置
	"os"
)

// Config 是一个配置加载器，封装了 viper 的功能。
type Config struct {
	v      *viper.Viper
	opts   *Options
	loaded bool
	mu     sync.RWMutex
}

// Logger 定义了一个简单的日志接口，以便 gconfig 可以集成应用程序的日志系统。
type Logger interface {
	Printf(format string, v ...interface{})
}

// DecoderOption 是一个用于在 Unmarshal 时配置解码器行为的声明式结构体。
type DecoderOption struct {
	// TagName 指定用于 unmarshal 的结构体标签名。
	TagName          string
	WeaklyTypedInput *bool // 是否开启弱类型转换 (e.g., string to int)。使用指针以区分 "未设置" 和 "设置为 false"。
	ErrorUnused      *bool // 如果为 true，当目标结构体中没有对应的字段时会报错。
	// DecodeHooks 是一组自定义解码钩子，用于处理复杂或自定义的类型转换。
	DecodeHooks []mapstructure.DecodeHookFunc
}

// DecoderOptionFunc 是一个用于修改 DecoderOption 的函数。
type DecoderOptionFunc func(*DecoderOption)

// Unmarshaler 定义了一个可以将配置解析到结构体中的接口。
type Unmarshaler interface {
	Unmarshal(rawVal interface{}, opts ...DecoderOptionFunc) error
}

// Options 保存了创建 viper 实例所需的所有配置。
type Options struct {
	// 本地文件配置
	Name  string   // 配置文件名 (不带扩展名)
	Type  string   // 配置文件类型 (e.g., "yaml", "json")
	Paths []string // 配置文件搜索路径
	File  string   // 完整的配置文件路径，如果设置，将忽略 Name, Type, Paths

	// 环境变量配置
	EnvPrefix   string
	EnvReplacer *strings.Replacer

	// DecoderOption 是在 New() 时设置的默认解码器选项。
	DecoderOption *DecoderOption
	// 远程配置源 (e.g., etcd, consul)
	RemoteProvider string
	RemoteEndpoint string
	RemotePath     string

	// OnChangeCallback 是一个在本地或远程配置发生变化时触发的回调函数。
	OnChangeCallback func(c Unmarshaler)

	// 内部日志
	Logger Logger
}

// Option 是一个用于修改 Options 的函数。
type Option func(*Options)

// WithFile 指定一个完整的配置文件路径。
func WithFile(path string) Option {
	return func(o *Options) {
		o.File = path
	}
}

// WithName 设置配置文件名。
func WithName(name string) Option {
	return func(o *Options) {
		o.Name = name
	}
}

// WithType 设置配置文件类型。
func WithType(typ string) Option {
	return func(o *Options) {
		o.Type = typ
	}
}

// WithPaths 添加配置文件搜索路径。
func WithPaths(paths ...string) Option {
	return func(o *Options) {
		o.Paths = append(o.Paths, paths...)
	}
}

// WithEnvPrefix 设置环境变量前缀。
func WithEnvPrefix(prefix string) Option {
	return func(o *Options) {
		o.EnvPrefix = prefix
	}
}

// WithRemoteProvider 设置远程配置。
// provider: "etcd", "consul", "firestore", etc.
// endpoint: "http://127.0.0.1:2379"
// path: "/config/yourapp.yaml"
func WithRemoteProvider(provider, endpoint, path string) Option {
	return func(o *Options) {
		o.RemoteProvider = provider
		o.RemoteEndpoint = endpoint
		o.RemotePath = path
	}
}

// WithOnChangeCallback 设置一个在配置变更时触发的回调。
func WithOnChangeCallback(cb func(c Unmarshaler)) Option {
	return func(o *Options) {
		o.OnChangeCallback = cb
	}
}

// WithLogger 设置内部 logger。
func WithLogger(logger Logger) Option {
	return func(o *Options) {
		o.Logger = logger
	}
}

// WithDecoderOptions 设置默认的解码器选项。
func WithDecoderOptions(opts ...DecoderOptionFunc) Option {
	return func(o *Options) {
		if o.DecoderOption == nil {
			o.DecoderOption = &DecoderOption{}
		}
		for _, opt := range opts {
			opt(o.DecoderOption)
		}
	}
}

// WithTagName 返回一个设置了 TagName 的 DecoderOptionFunc。
func WithTagName(tagName string) DecoderOptionFunc {
	return func(opt *DecoderOption) {
		opt.TagName = tagName
	}
}

// WithWeaklyTypedInput 返回一个设置了弱类型转换开关的 DecoderOptionFunc。
func WithWeaklyTypedInput(enabled bool) DecoderOptionFunc {
	return func(opt *DecoderOption) {
		opt.WeaklyTypedInput = &enabled
	}
}

// WithErrorUnused 返回一个设置了 "ErrorUnused" 开关的 DecoderOptionFunc。
func WithErrorUnused(enabled bool) DecoderOptionFunc {
	return func(opt *DecoderOption) {
		opt.ErrorUnused = &enabled
	}
}

// WithDecodeHooks 返回一个设置了自定义解码钩子的 DecoderOptionFunc。
func WithDecodeHooks(hooks ...mapstructure.DecodeHookFunc) DecoderOptionFunc {
	return func(opt *DecoderOption) {
		opt.DecodeHooks = append(opt.DecodeHooks, hooks...)
	}
}

// New 根据提供的选项创建一个配置好的 *Config 实例。
// 它会从文件、环境变量和远程源加载配置。
//
// 加载优先级: 环境变量 > 配置文件 > 默认值
func New(opts ...Option) (*Config, error) {
	options := &Options{
		Name:        "config",
		Type:        "yaml",
		Paths:       []string{".", "/etc/yourapp/"},
		EnvPrefix:   "APP",
		EnvReplacer: strings.NewReplacer(".", "_"),
		// 默认使用 "json" 标签
		DecoderOption: &DecoderOption{
			TagName: "json",
		},
		Logger: &defaultLogger{},
	}
	for _, opt := range opts {
		opt(options)
	}

	v := viper.New()

	if options.File != "" {
		v.SetConfigFile(options.File)
	} else {
		v.SetConfigName(options.Name)
		v.SetConfigType(options.Type)
		for _, path := range options.Paths {
			v.AddConfigPath(path)
		}
	}

	v.SetEnvPrefix(options.EnvPrefix)
	v.SetEnvKeyReplacer(options.EnvReplacer)
	v.AutomaticEnv()

	c := &Config{v: v, opts: options}
	return c, nil
}

// defaultLogger 是一个简单的默认日志实现。
type defaultLogger struct{}

func (l *defaultLogger) Printf(format string, v ...interface{}) {
	fmt.Printf(format+"\n", v...)
}

// Unmarshal 将已加载的配置解析到 target 结构体中。
func (c *Config) Unmarshal(target interface{}, opts ...DecoderOptionFunc) error {
	c.mu.RLock()
	if !c.loaded {
		c.mu.RUnlock() // 释放读锁，以便 Load 方法可以获取写锁
		if err := c.Load(); err != nil {
			return err
		}
	} else {
		c.mu.RUnlock()
	}

	// 从全局选项克隆一份解码器选项
	finalOpt := &DecoderOption{}
	if c.opts.DecoderOption != nil {
		// copy
		*finalOpt = *c.opts.DecoderOption
	}

	// 应用所有传入的 Unmarshal 选项
	for _, opt := range opts {
		opt(finalOpt)
	}

	// 将我们声明式的 DecoderOption 结构体转换为 viper 需要的函数式选项。
	return c.v.Unmarshal(target, c.buildViperDecoderOptions(finalOpt)...)
}

// buildViperDecoderOptions 是一个内部转换函数。
func (c *Config) buildViperDecoderOptions(opt *DecoderOption) []viper.DecoderConfigOption {
	if opt == nil {
		return nil
	}
	// 创建一个临时的 mapstructure.DecoderConfig 来收集所有 DecoderOption 的设置
	// 这样可以确保后传入的选项能够覆盖先传入的选项
	tempMsConfig := &mapstructure.DecoderConfig{}

	// 应用所有 DecoderOption 的设置
	if opt.TagName != "" {
		tempMsConfig.TagName = opt.TagName
	}
	if opt.WeaklyTypedInput != nil {
		tempMsConfig.WeaklyTypedInput = *opt.WeaklyTypedInput
	}
	if opt.ErrorUnused != nil {
		tempMsConfig.ErrorUnused = *opt.ErrorUnused
	}
	if len(opt.DecodeHooks) > 0 {
		tempMsConfig.DecodeHook = mapstructure.ComposeDecodeHookFunc(opt.DecodeHooks...)
	}

	// 将收集到的设置封装成一个 viper.DecoderConfigOption
	return []viper.DecoderConfigOption{func(cfg *mapstructure.DecoderConfig) {
		// 将临时配置的设置复制到实际的 cfg 中
		if tempMsConfig.TagName != "" {
			cfg.TagName = tempMsConfig.TagName
		}
		if tempMsConfig.WeaklyTypedInput { // 默认已经是 true，但为了明确，我们还是设置
			cfg.WeaklyTypedInput = tempMsConfig.WeaklyTypedInput
		}
		if tempMsConfig.ErrorUnused {
			cfg.ErrorUnused = tempMsConfig.ErrorUnused
		}
		if tempMsConfig.DecodeHook != nil {
			cfg.DecodeHook = tempMsConfig.DecodeHook
		}
	}}
}

// SetDefault 设置配置项的默认值。
func (c *Config) SetDefault(key string, value interface{}) {
	c.v.SetDefault(key, value)
}

// GetString 获取一个字符串类型的配置项。
func (c *Config) GetString(key string) string {
	return c.v.GetString(key)
}

// AllSettings 返回所有配置项的 map。
func (c *Config) AllSettings() map[string]interface{} {
	return c.v.AllSettings()
}

// Load 执行实际的配置加载操作。
func (c *Config) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果已经加载过，则直接返回
	if c.loaded {
		return nil
	}

	v := c.v
	options := c.opts

	// 读取本地配置文件
	if err := v.ReadInConfig(); err != nil {
		// 仅当错误不是 "文件未找到" 时才返回错误
		var nfErr viper.ConfigFileNotFoundError
		var pathErr *os.PathError
		if !errors.As(err, &nfErr) && !errors.As(err, &pathErr) {
			return fmt.Errorf("failed to read config file: %w", err)
		}
		options.Logger.Printf("Config file not found, proceeding without it. Searched in paths: %v for %s.%s", options.Paths, options.Name, options.Type)
	}

	// (可选) 读取远程配置
	if options.RemoteProvider != "" && options.RemoteEndpoint != "" && options.RemotePath != "" {
		if err := v.AddRemoteProvider(options.RemoteProvider, options.RemoteEndpoint, options.RemotePath); err != nil {
			return fmt.Errorf("failed to add remote provider: %w", err)
		}
		v.SetConfigType(options.Type) // 远程配置也需要指定类型，如 "yaml"
		if err := v.ReadRemoteConfig(); err != nil {
			// 如果远程配置读取失败，可以根据策略选择是报错还是继续
			options.Logger.Printf("Warning: failed to read remote config, proceeding with local/env config. Error: %v", err)
		}
	}

	// 启动监控
	c.watch()

	// 标记为已加载
	c.loaded = true

	return nil
}

// watch 启动对本地和远程配置的监控。
func (c *Config) watch() {
	v := c.v
	options := c.opts

	// 监控本地文件变化
	v.OnConfigChange(func(e fsnotify.Event) {
		options.Logger.Printf("Local config file changed: %s. Triggering callback...", e.Name)
		if options.OnChangeCallback != nil {
			options.OnChangeCallback(c)
		}
	})
	v.WatchConfig()

	// 监控远程配置变化
	if options.RemoteProvider != "" {
		go func() {
			for {
				if err := v.WatchRemoteConfigOnChannel(); err != nil {
					options.Logger.Printf("Error watching remote config, retrying...: %v", err)
					continue
				}
				options.Logger.Printf("Remote config changed. Triggering callback.")
				if options.OnChangeCallback != nil {
					options.OnChangeCallback(c)
				}
			}
		}()
	}
}
