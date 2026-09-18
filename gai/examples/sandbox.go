//go:build ignore

// 新 SDK API 的 Go 设计示例；对应接口尚未实现，暂不参与构建。
// Go design example for the new SDK API; excluded until the interfaces are implemented.
package examples

import (
	"context"

	"github.com/sofiworker/gk/gai/agent"
	"github.com/sofiworker/gk/gai/hook"
	"github.com/sofiworker/gk/gai/model"
	"github.com/sofiworker/gk/gai/sandbox/memory"
	"github.com/sofiworker/gk/gai/tool"
	"github.com/sofiworker/gk/gai/tool/builtin"
)

// RunWithSandbox 借用调用方创建的环境；会话关闭不销毁该环境。
// RunWithSandbox borrows the caller's environment; closing the session does not destroy it.
func RunWithSandbox(ctx context.Context, chatModel model.Model, box *memory.Sandbox) (string, error) {
	a, err := agent.New(
		agent.WithModel(chatModel),
		agent.WithInstructions("只在提供的工作环境中操作文件。"),
		agent.WithTools(builtin.Files()...),
		agent.WithHooks(hook.ToolPolicy(allowFileTools)),
	)
	if err != nil {
		return "", err
	}
	session, err := a.NewSession(ctx, agent.WithSandbox(box))
	if err != nil {
		return "", err
	}
	defer session.Close()
	result, err := session.Run(ctx, agent.Text("创建 note.txt，写入 hello，然后读取确认"))
	if err != nil {
		return "", err
	}
	return result.Text(), nil
}

// 工具授权不扩大 Sandbox 的路径和资源权限。
// Tool authorization does not expand Sandbox path or resource permissions.
func allowFileTools(ctx context.Context, call tool.Authorization) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch call.Tool.ID {
	case "read_file", "write_file":
		return nil
	default:
		return tool.ErrDenied
	}
}
