package app

// Config 定义测试应用配置。
// Config defines the test application configuration.
type Config struct {
	StaticDir    string
	MaxBodyBytes int64
	Secret       string
}
