//go:build ignore

// 新 SDK API 的 Go 设计示例；对应接口尚未实现，暂不参与构建。
// Go design example for the new SDK API; excluded until the interfaces are implemented.
package examples

import (
	"context"

	"github.com/sofiworker/gk/gai/agent"
	"github.com/sofiworker/gk/gai/model"
)

// RunSession 的第二次请求自动包含同一会话的历史。
// RunSession automatically includes conversation history in its second request.
func RunSession(ctx context.Context, chatModel model.Model) (string, error) {
	a, err := agent.New(
		agent.WithModel(chatModel),
		agent.WithInstructions("你是 Go 开发助手。"),
	)
	if err != nil {
		return "", err
	}
	session, err := a.NewSession(ctx)
	if err != nil {
		return "", err
	}
	defer session.Close()

	if _, err := session.Run(ctx, agent.Text("我需要一个支持取消的后台任务，请先给出设计")); err != nil {
		return "", err
	}
	result, err := session.Run(ctx, agent.Text("按刚才的设计给出 Go 实现"))
	if err != nil {
		return "", err
	}
	return result.Text(), nil
}
