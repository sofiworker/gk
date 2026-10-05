package ghttp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/sofiworker/gk/ghttp/wire"
)

// ColorMode 控制访问日志的 ANSI 颜色输出。
// ColorMode controls ANSI colors in the access log.
type ColorMode int

const (
	// ColorAuto 仅当输出为终端（字符设备）时启用颜色；输出到 Logger 时从不着色。
	// ColorAuto enables colors only when the output is a terminal (character device);
	// output to a Logger is never colored.
	ColorAuto ColorMode = iota
	// ColorAlways 始终着色。
	// ColorAlways always colors.
	ColorAlways
	// ColorNever 从不着色。
	// ColorNever never colors.
	ColorNever
)

// AccessLogConfig 定义访问日志中间件的配置。
// AccessLogConfig configures the access log middleware.
type AccessLogConfig struct {
	// Logger 非 nil 时日志写入 Logger：5xx 用 Errorf，4xx 用 Warnf，其余用 Infof；此时忽略 Output。
	// When Logger is non-nil the line goes to it: Errorf for 5xx, Warnf for 4xx, Infof
	// otherwise; Output is ignored.
	Logger Logger

	// Output 是日志写入的目标，默认 os.Stdout
	// Output is where lines are written, os.Stdout by default
	Output io.Writer

	// Color 控制颜色，默认 ColorAuto
	// Color controls coloring, ColorAuto by default
	Color ColorMode

	// SkipPaths 是跳过日志的请求路径
	// SkipPaths are request paths not logged
	SkipPaths []string

	// Formatter 是自定义格式化函数，返回一行日志（Output 模式下需自带换行）
	// Formatter is a custom formatter returning one line (include the newline in Output mode)
	Formatter func(AccessLogParams) string
}

// AccessLogParams 包含访问日志格式化所需的参数。所有字符串字段都已转义控制字符。
// AccessLogParams holds the access log fields. All string fields have control characters escaped.
type AccessLogParams struct {
	// Request 是原始请求
	// Request is the original request
	Request *http.Request

	// TimeStamp 是响应完成时间
	// TimeStamp is when the response finished
	TimeStamp time.Time

	// StatusCode 是最终状态码；handler 返回错误且未写出响应时取 StatusFromError(err)
	// StatusCode is the final status; StatusFromError(err) when the handler failed before writing
	StatusCode int

	// Latency 是请求处理时间
	// Latency is the processing time
	Latency time.Duration

	// Method、Path 是请求方法与路径
	// Method and Path are the request method and path
	Method string
	Path   string

	// Route 是匹配到的路由模板（如 /users/:id），未匹配时为空；按路由聚合时应使用它而不是 Path
	// Route is the matched template (e.g. /users/:id), empty when unmatched; aggregate by it, not Path
	Route string

	// ClientIP 是客户端地址（配合 RealIP 中间件可得到真实 IP）
	// ClientIP is the client address (the real IP when used with RealIP)
	ClientIP string

	// BodySize 是响应体字节数
	// BodySize is the number of response body bytes
	BodySize int64

	// ErrorMessage 是 handler 返回的错误文本
	// ErrorMessage is the text of the error returned by the handler
	ErrorMessage string

	// Color 报告格式化时是否应着色
	// Color reports whether the formatter should color the line
	Color bool
}

// AccessLog 返回写入 os.Stdout 的访问日志中间件。
// AccessLog returns an access log middleware writing to os.Stdout.
func AccessLog() Middleware {
	return AccessLogWithConfig(AccessLogConfig{})
}

// AccessLogWithWriter 返回写入 out 的访问日志中间件。
// AccessLogWithWriter returns an access log middleware writing to out.
func AccessLogWithWriter(out io.Writer) Middleware {
	return AccessLogWithConfig(AccessLogConfig{Output: out})
}

// AccessLogWithLogger 返回写入 Logger 的访问日志中间件。
// AccessLogWithLogger returns an access log middleware writing to l.
func AccessLogWithLogger(l Logger) Middleware {
	return AccessLogWithConfig(AccessLogConfig{Logger: l})
}

// AccessLogWithConfig 返回按配置记录访问日志的中间件。
// AccessLogWithConfig returns an access log middleware using config.
func AccessLogWithConfig(config AccessLogConfig) Middleware {
	out := config.Output
	if out == nil {
		out = os.Stdout
	}
	formatter := config.Formatter
	if formatter == nil {
		formatter = defaultAccessLogFormatter
	}
	color := config.Logger == nil && useColor(config.Color, out)
	skip := make(map[string]struct{}, len(config.SkipPaths))
	for _, p := range config.SkipPaths {
		skip[p] = struct{}{}
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, req *Request, resp *Response) error {
			path := req.Raw.URL.Path
			if _, ok := skip[path]; ok {
				return next(ctx, req, resp)
			}
			start := time.Now()
			err := next(ctx, req, resp)

			params := AccessLogParams{
				Request:    req.Raw,
				TimeStamp:  time.Now(),
				StatusCode: FinalStatus(resp, err),
				Latency:    time.Since(start),
				Method:     wire.LogToken(req.Raw.Method),
				Path:       wire.LogToken(path),
				Route:      req.Route(),
				ClientIP:   wire.LogToken(ClientIP(req)),
				BodySize:   resp.Size(),
				Color:      color,
			}
			// 错误文本可能包含来自查询串等客户端输入的换行，单行化防止伪造日志记录
			// Error text may carry newlines from client input such as the query; keep it on one line
			if err != nil {
				params.ErrorMessage = wire.LogToken(err.Error())
			}

			line := formatter(params)
			if l := config.Logger; l != nil {
				switch {
				case params.StatusCode >= 500:
					l.Errorf("%s", line)
				case params.StatusCode >= 400:
					l.Warnf("%s", line)
				default:
					l.Infof("%s", line)
				}
			} else {
				_, _ = io.WriteString(out, line)
			}
			return err
		}
	}
}

// useColor 根据模式与输出目标决定是否着色。
// useColor decides coloring from the mode and the output.
func useColor(mode ColorMode, out io.Writer) bool {
	switch mode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	}
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// defaultAccessLogFormatter 是默认格式：时间 | 状态 | 耗时 | 客户端 | 方法 路径 [| error: ...]。
// defaultAccessLogFormatter is the default format: time | status | latency | client | method path [| error: ...].
func defaultAccessLogFormatter(p AccessLogParams) string {
	statusColor, methodColor, reset := "", "", ""
	if p.Color {
		statusColor, methodColor, reset = statusColorOf(p.StatusCode), methodColorOf(p.Method), "\033[0m"
	}
	errMsg := ""
	if p.ErrorMessage != "" {
		errMsg = " | error: " + p.ErrorMessage
	}
	return fmt.Sprintf("%s |%s %3d %s| %13v | %s |%s %-7s %s %s%s\n",
		p.TimeStamp.Format("2006/01/02 - 15:04:05"),
		statusColor, p.StatusCode, reset,
		p.Latency,
		p.ClientIP,
		methodColor, p.Method, reset,
		p.Path,
		errMsg,
	)
}

// statusColorOf 返回状态码对应的颜色。
// statusColorOf returns the color for a status code.
func statusColorOf(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "\033[97;42m"
	case code >= 300 && code < 400:
		return "\033[90;47m"
	case code >= 400 && code < 500:
		return "\033[90;43m"
	default:
		return "\033[97;41m"
	}
}

// methodColorOf 返回 HTTP 方法对应的颜色。
// methodColorOf returns the color for an HTTP method.
func methodColorOf(method string) string {
	switch method {
	case http.MethodGet:
		return "\033[97;44m"
	case http.MethodPost:
		return "\033[97;46m"
	case http.MethodPut:
		return "\033[90;43m"
	case http.MethodDelete:
		return "\033[97;41m"
	case http.MethodPatch, http.MethodHead:
		return "\033[97;45m"
	case http.MethodOptions:
		return "\033[90;47m"
	default:
		return ""
	}
}
