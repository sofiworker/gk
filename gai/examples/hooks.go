//go:build ignore

// 新 SDK API 的 Go 设计示例；对应接口尚未实现，暂不参与构建。
// Go design example for the new SDK API; excluded until the interfaces are implemented.
package examples

import (
	"context"
	"errors"
	"strings"

	"github.com/sofiworker/gk/gai/agent"
	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/hook"
	"github.com/sofiworker/gk/gai/model"
)

// RunWithHooks 展示每次模型请求前配置参数、响应后转换及业务校验。
// RunWithHooks configures each model request and transforms and validates responses.
func RunWithHooks(ctx context.Context, chatModel model.Model) (string, error) {
	a, err := agent.New(
		agent.WithModel(chatModel),
		agent.WithInstructions("用简洁的语言回答问题。"),
		agent.WithHooks(
			hook.BeforeModel(func(ctx context.Context, event hook.ModelRequest) (model.Request, error) {
				if err := ctx.Err(); err != nil {
					return model.Request{}, err
				}
				request := event.Request
				request.Parameters.Temperature = model.Value(0.2)
				request.Parameters.MaxOutputTokens = model.Value(1024)
				return request, nil
			}),
			hook.AfterModel(func(ctx context.Context, event hook.ModelResponse) (model.Response, error) {
				if err := ctx.Err(); err != nil {
					return model.Response{}, err
				}
				response := event.Response
				for i := range response.Message.Content {
					part := &response.Message.Content[i]
					if part.Kind == core.ContentText {
						part.Text = strings.TrimSpace(part.Text)
					}
				}
				// 仅校验最终回答；工具请求不能被当成空答案拒绝。
				// Validate final answers only; tool requests are not empty answers.
				if response.FinishReason == model.FinishStop {
					hasText := false
					for _, part := range response.Message.Content {
						hasText = hasText || (part.Kind == core.ContentText && part.Text != "")
					}
					if !hasText {
						return response, errors.New("final answer must contain text")
					}
				}
				return response, nil
			}),
		),
	)
	if err != nil {
		return "", err
	}
	result, err := a.Run(ctx, agent.Text("如何优雅关闭 Go HTTP 服务？"))
	if err != nil {
		return "", err
	}
	return result.Text(), nil
}
