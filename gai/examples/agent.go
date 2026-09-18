//go:build ignore

// 新 SDK API 的 Go 设计示例；对应接口尚未实现，暂不参与构建。
// Go design example for the new SDK API; excluded until the interfaces are implemented.
package examples

import (
	"context"

	"github.com/sofiworker/gk/gai/agent"
	"github.com/sofiworker/gk/gai/model"
)

// RunAgent 使用调用方已配置的模型，执行一次独立请求。
// RunAgent executes one independent request using an application-configured model.
func RunAgent(ctx context.Context, chatModel model.Model) (string, error) {
	a, err := agent.New(
		agent.WithModel(chatModel),
		agent.WithInstructions("你是开发助手，用简洁的语言回答问题。"),
	)
	if err != nil {
		return "", err
	}
	result, err := a.Run(ctx, agent.Text("解释 Go 的 context.Context 有什么作用"))
	if err != nil {
		return "", err
	}
	return result.Text(), nil
}
