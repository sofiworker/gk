package runtime_test

import (
	"context"
	"fmt"

	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/model"
	runtime "github.com/sofiworker/gk/gai/runtime"
	"github.com/sofiworker/gk/gai/session/memory"
)

func ExampleRuntime_Run() {
	ctx := context.Background()
	client := modelFunc(func(context.Context, model.Request) (model.Response, error) {
		return model.Response{FinishReason: model.FinishStop, Message: model.Message{Role: core.RoleAssistant, Content: []core.Content{{Kind: core.ContentText, Text: "hello"}}}}, nil
	})
	store := memory.New()
	rt, err := runtime.New(core.Agent{ID: "assistant", Version: "1"}, client, store)
	if err != nil {
		panic(err)
	}
	sess, err := rt.CreateSession(ctx, core.SessionMetadata{Model: core.ModelSelection{ID: "logical"}})
	if err != nil {
		panic(err)
	}
	turn, err := rt.Run(ctx, sess.ID, runtime.Input{Content: []core.Content{{Kind: core.ContentText, Text: "hi"}}})
	if err != nil {
		panic(err)
	}
	fmt.Println(turn.Status, turn.Messages[1].Content[0].Text)
	saved, err := store.Load(ctx, sess.ID)
	if err != nil {
		panic(err)
	}
	fmt.Println(len(saved.Turns), len(saved.Turns[0].ModelCalls))
	// Output:
	// completed hello
	// 1 1
}
