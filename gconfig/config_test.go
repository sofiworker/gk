package gconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestAppConfig 是一个用于测试的示例配置结构体，使用 json 标签。
type TestAppConfig struct {
	App struct {
		Name string `json:"name"`
		Env  string `json:"env"`
	} `json:"app"`
	Database struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		User     string `json:"user"`
		Password string `json:"password"`
		DBName   string `json:"dbname"`
	} `json:"database"`
	FeatureFlag bool `json:"feature_flag"`
}

// setupTestFile 创建一个临时的配置文件用于测试。
func setupTestFile(t *testing.T, dir, filename, content string) string {
	assert := assert.New(t)
	configFilePath := filepath.Join(dir, filename)
	err := os.WriteFile(configFilePath, []byte(content), 0644)
	assert.NoError(err)
	return configFilePath
}

func TestConfigLoading(t *testing.T) {
	assert := assert.New(t)

	// 创建临时目录和配置文件
	tempDir, err := os.MkdirTemp("", "gconfig_test")
	assert.NoError(err)
	defer os.RemoveAll(tempDir)

	t.Run("Initial Load with File and Env Override", func(t *testing.T) {
		// 准备初始配置文件
		configContent := "" +
			"app:\n" +
			"  name: \"MyAppFromFile\"\n" +
			"  env: \"development\"\n" +
			"database:\n" +
			"  host: \"localhost\"\n" +
			"  port: 5432\n" +
			"  user: \"file_user\"\n" +
			"  password: \"file_password\"\n" +
			"  dbname: \"testdb\"\n" +
			"feature_flag: false\n"
		configFilePath := setupTestFile(t, tempDir, "config1.yaml", configContent)

		// 设置环境变量，模拟覆盖
		os.Setenv("APP_APP_ENV", "production")
		os.Setenv("APP_DATABASE_PASSWORD", "env_secret_password")
		os.Setenv("APP_FEATURE_FLAG", "true")
		os.Setenv("APP_DATABASE_PORT", "6432") // 覆盖文件中的端口
		defer func() {
			os.Unsetenv("APP_APP_ENV")
			os.Unsetenv("APP_DATABASE_PASSWORD")
			os.Unsetenv("APP_FEATURE_FLAG")
			os.Unsetenv("APP_DATABASE_PORT")
		}()

		// 创建加载器并显式加载
		loader, err := New(WithFile(configFilePath)) // 默认使用 json 标签
		assert.NoError(err)
		err = loader.Load()
		assert.NoError(err)

		// Unmarshal并验证
		var cfg TestAppConfig
		err = loader.Unmarshal(&cfg)
		assert.NoError(err)

		assert.Equal("MyAppFromFile", cfg.App.Name, "App.Name should come from file")
		assert.Equal("production", cfg.App.Env, "App.Env should be overridden by environment variable")
		assert.Equal("localhost", cfg.Database.Host, "Database.Host should come from file")
		assert.Equal(6432, cfg.Database.Port, "Database.Port should be overridden by environment variable")
		assert.Equal("file_user", cfg.Database.User, "Database.User should come from file")
		assert.Equal("env_secret_password", cfg.Database.Password, "Database.Password should be overridden by environment variable")
		assert.Equal("testdb", cfg.Database.DBName, "Database.DBName should come from file")
		assert.True(cfg.FeatureFlag, "FeatureFlag should be overridden by environment variable")
	})
}

func TestHotReloadOnFileChange(t *testing.T) {
	assert := assert.New(t)
	tempDir, err := os.MkdirTemp("", "gconfig_hot_reload")
	assert.NoError(err)
	defer os.RemoveAll(tempDir)

	// 准备初始配置文件
	configContent := "" +
		"app:\n" +
		"  name: \"InitialApp\"\n" +
		"database:\n" +
		"  host: \"initial_host\"\n" +
		"  password: \"initial_password\"\n"
	configFilePath := setupTestFile(t, tempDir, "config2.yaml", configContent)

	// 设置环境变量，确保其优先级在热加载后依然最高
	os.Setenv("APP_DATABASE_PASSWORD", "env_password_always_wins")
	defer os.Unsetenv("APP_DATABASE_PASSWORD")

	var cfg TestAppConfig
	var wg sync.WaitGroup
	var once sync.Once
	wg.Add(1)

	// 定义回调函数，当配置变更时，它会重新 unmarshal 并通知测试完成
	reloadCallback := func(c Unmarshaler) {
		fmt.Println("Hot reload callback triggered!")
		err := c.Unmarshal(&cfg)
		assert.NoError(err)
		once.Do(func() {
			wg.Done()
		})
	}

	// 创建加载器，并进行首次加载
	loader, err := New(WithFile(configFilePath), WithOnChangeCallback(reloadCallback))
	assert.NoError(err)
	err = loader.Unmarshal(&cfg) // 隐式调用 Load()
	assert.NoError(err)

	// 验证首次加载
	assert.Equal("InitialApp", cfg.App.Name)
	assert.Equal("initial_host", cfg.Database.Host)
	assert.Equal("env_password_always_wins", cfg.Database.Password)

	// 准备新的配置内容并写入文件，以触发热加载
	newConfigContent := "" +
		"app:\n" +
		"  name: \"ReloadedApp\"\n" +
		"database:\n" +
		"  host: \"reloaded_host\"\n" +
		"  password: \"reloaded_file_password\"\n"
	time.Sleep(100 * time.Millisecond) // 等待 watcher 启动
	err = os.WriteFile(configFilePath, []byte(newConfigContent), 0644)
	assert.NoError(err)

	// 等待回调函数执行完成
	wg.Wait()

	// 验证热加载后的配置
	assert.Equal("ReloadedApp", cfg.App.Name, "App.Name should be reloaded from file")
	assert.Equal("reloaded_host", cfg.Database.Host, "Database.Host should be reloaded from file")
	assert.Equal("env_password_always_wins", cfg.Database.Password, "Database.Password should still be overridden by environment variable after reload")
}

func TestConfigFileNotFound(t *testing.T) {
	assert := assert.New(t)

	// 尝试加载一个不存在的配置文件，应该不返回错误
	loader, err := New(WithFile("/path/to/non_existent_config.yaml"), WithName("non_existent"))
	assert.NoError(err, "New() should not return an error for a non-existent file path")
	assert.NotNil(loader)

	var cfg TestAppConfig
	// 第一次 Unmarshal 会触发 Load, Load 应该能处理文件未找到的错误
	err = loader.Unmarshal(&cfg)
	assert.NoError(err, "Unmarshal (and implicit Load) should not fail if config file is not found")

	// 验证此时 cfg 应该是其零值
	assert.Equal("", cfg.App.Name)
	assert.Equal(0, cfg.Database.Port)
}

func TestUnmarshalError(t *testing.T) {
	assert := assert.New(t)

	tempDir, err := os.MkdirTemp("", "gconfig_unmarshal_test")
	assert.NoError(err)
	defer os.RemoveAll(tempDir)

	configFilePath := filepath.Join(tempDir, "config.yaml")
	// 提供一个与 TestAppConfig 不兼容的配置内容，例如将 port 设置为字符串
	configContent := `database: { port: "invalid_port_string" }`
	err = os.WriteFile(configFilePath, []byte(configContent), 0644)
	assert.NoError(err)

	loader, err := New(WithFile(configFilePath))
	assert.NoError(err)

	var cfg TestAppConfig
	// Unmarshal 应该失败，因为 port 字段类型不匹配
	err = loader.Unmarshal(&cfg, WithWeaklyTypedInput(false)) // 禁用弱类型转换以确保失败
	assert.Error(err)
	assert.Contains(err.Error(), "cannot parse value as 'int'")
}

func TestUnmarshalImplicitLoad(t *testing.T) {
	assert := assert.New(t)

	tempDir, err := os.MkdirTemp("", "gconfig_implicit_load")
	assert.NoError(err)
	defer os.RemoveAll(tempDir)

	configContent := `app: { name: "ImplicitLoadApp" }`
	configFilePath := setupTestFile(t, tempDir, "config3.yaml", configContent)

	loader, err := New(WithFile(configFilePath))
	assert.NoError(err)
	assert.False(loader.loaded, "Config should not be loaded after New()")

	var cfg TestAppConfig
	err = loader.Unmarshal(&cfg)
	assert.NoError(err)

	assert.True(loader.loaded, "Config should be loaded after first Unmarshal()")
	assert.Equal("ImplicitLoadApp", cfg.App.Name)
}

func TestUnmarshalWithDefaultDecoderOptions(t *testing.T) {
	assert := assert.New(t)

	// 定义一个使用 `json` 标签的结构体
	type JsonTaggedConfig struct {
		ServerPort    int    `json:"server-port"`
		ServerMode    string `json:"server_mode"`
		EnableFeature bool   `json:"enable_feature"`
	}

	configContent := `
server-port: "9090"
server_mode: "debug"
enable_feature: "true"
`
	tempDir, err := os.MkdirTemp("", "gconfig_default_tag")
	assert.NoError(err)
	defer os.RemoveAll(tempDir)
	configFilePath := setupTestFile(t, tempDir, "config4.yaml", configContent)

	// 创建加载器，使用默认的解码器选项 (json 标签, 弱类型转换)
	loader, err := New(WithFile(configFilePath), WithDecoderOptions(WithWeaklyTypedInput(true)))
	assert.NoError(err)

	var cfg JsonTaggedConfig
	err = loader.Unmarshal(&cfg)
	assert.NoError(err)

	assert.Equal(9090, cfg.ServerPort, "Should map 'server-port' and convert string to int")
	assert.Equal("debug", cfg.ServerMode, "Should map 'server_mode'")
	assert.True(cfg.EnableFeature, "Should map 'enable_feature' and convert string to bool")
}

func TestUnmarshalWithPerCallDecoderOption(t *testing.T) {
	assert := assert.New(t)

	// 定义一个使用 `yaml` 标签的结构体
	type YamlTaggedConfig struct {
		ServerPort int `yaml:"serverPort"`
	}

	configContent := `serverPort: 8888`
	tempDir, err := os.MkdirTemp("", "gconfig_override_tag")
	assert.NoError(err)
	defer os.RemoveAll(tempDir)
	configFilePath := setupTestFile(t, tempDir, "config5.yaml", configContent)

	// 创建加载器时，默认是 json 标签
	loader, err := New(WithFile(configFilePath))
	assert.NoError(err)

	// 在 Unmarshal 调用时，传入一个临时的解码器选项来使用 "yaml" 标签。
	var cfg YamlTaggedConfig
	err = loader.Unmarshal(&cfg, WithTagName("yaml"))
	assert.NoError(err)

	assert.Equal(8888, cfg.ServerPort, "Should use 'yaml' tag to unmarshal")

	// 再次调用，不带选项，应该无法解析
	var cfg2 YamlTaggedConfig
	err = loader.Unmarshal(&cfg2)
	assert.NoError(err) // 默认 TagName="json" 仍会匹配字段名并解码
	assert.Equal(8888, cfg2.ServerPort, "Default decode should still map field name")
}

func TestDecoderOptionsMerging(t *testing.T) {
	assert := assert.New(t)

	type Config struct {
		Host  string `json:"host"`
		Extra string `json:"extra"` // This field does not exist in the config file
	}

	configContent := "host: \"localhost\"\nunknown: true\n"
	tempDir, err := os.MkdirTemp("", "gconfig_merging_test")
	assert.NoError(err)
	defer os.RemoveAll(tempDir)
	configFilePath := setupTestFile(t, tempDir, "config.yaml", configContent)

	// 创建一个加载器，并设置全局选项：当有未使用的键时报错
	loader, err := New(
		WithFile(configFilePath),
		WithDecoderOptions(WithErrorUnused(true)), // 全局选项
	)
	assert.NoError(err)

	var cfg Config

	// 第一次 Unmarshal: 应该失败，因为 'Extra' 字段在配置文件中不存在
	err = loader.Unmarshal(&cfg)
	assert.Error(err, "Unmarshal should fail with ErrorUnused=true")
	assert.Contains(err.Error(), "invalid keys")

	// 第二次 Unmarshal: 在本次调用中覆盖解码器选项，允许未使用键
	err = loader.Unmarshal(&cfg, WithErrorUnused(false)) // 单次调用覆盖
	assert.NoError(err, "Unmarshal should succeed with ErrorUnused=false override")
	assert.Equal("localhost", cfg.Host)
}
