// Package runtime 连接 Agent、会话存储与隔离环境，执行并记录完整轮次。
// Package runtime connects agents, session storage and environments to execute and record turns.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/sofiworker/gk/gai/agent"
	"github.com/sofiworker/gk/gai/core"
	"github.com/sofiworker/gk/gai/id"
	"github.com/sofiworker/gk/gai/model"
	sandbox "github.com/sofiworker/gk/gai/sandbox/memory"
	"github.com/sofiworker/gk/gai/session"
)

var (
	ErrConfig      = errors.New("invalid runtime configuration")
	ErrEnvironment = errors.New("runtime environment unavailable")
	ErrNotRunning  = errors.New("turn is not running in this runtime")
	ErrHistory     = errors.New("session has unresolved tool calls")
)

type Capabilities func(context.Context, core.Call) (core.ToolContext, error)

// Environment 由可信应用解析环境，返回实际版本及按调用创建的能力视图。
// Environment resolves environments in trusted application code and returns actual versions and per-call views.
type Environment func(context.Context, core.EnvironmentBinding) (core.EnvironmentBinding, Capabilities, error)
type config struct {
	environment Environment
	runner      []agent.Option
}
type Option func(*config) error

func WithEnvironment(resolve Environment) Option {
	return func(c *config) error {
		if resolve == nil {
			return ErrConfig
		}
		c.environment = resolve
		return nil
	}
}
func WithAgentOptions(options ...agent.Option) Option {
	return func(c *config) error { c.runner = append([]agent.Option(nil), options...); return nil }
}

type running struct {
	turnID string
	cancel context.CancelFunc
}
type Runtime struct {
	runner      *agent.Runner
	store       session.Store
	environment Environment
	mu          sync.Mutex
	active      map[string]running
	boxes       map[string]*sandbox.Sandbox
}

func New(definition core.Agent, client model.Client, store session.Store, options ...Option) (*Runtime, error) {
	if store == nil || definition.Version == "" {
		return nil, ErrConfig
	}
	c := config{}
	for _, o := range options {
		if o == nil {
			return nil, ErrConfig
		}
		if err := o(&c); err != nil {
			return nil, err
		}
	}
	runner, err := agent.New(definition, client, c.runner...)
	if err != nil {
		return nil, err
	}
	return &Runtime{runner: runner, store: store, environment: c.environment, active: make(map[string]running), boxes: make(map[string]*sandbox.Sandbox)}, nil
}

// CreateSession 默认创建独立内存 Sandbox；外部环境通过 WithEnvironment 解析。
// CreateSession allocates an independent memory sandbox by default; WithEnvironment resolves external environments.
func (r *Runtime) CreateSession(ctx context.Context, metadata core.SessionMetadata) (core.Session, error) {
	if metadata.Model.ID == "" {
		return core.Session{}, ErrConfig
	}
	binding := r.runner.Binding()
	if metadata.Agent.ID != "" && metadata.Agent != binding {
		return core.Session{}, ErrConfig
	}
	metadata.Agent = binding
	key := id.NewV7()
	var box *sandbox.Sandbox
	if r.environment == nil {
		if metadata.Environment.ID != "" {
			return core.Session{}, ErrEnvironment
		}
		var err error
		box, err = sandbox.New(sandbox.WithID(id.NewV7()))
		if err != nil {
			return core.Session{}, err
		}
		state, err := box.State(ctx)
		if err != nil {
			return core.Session{}, err
		}
		metadata.Environment = core.EnvironmentBinding{ID: state.ID, Revision: state.Revision, PolicyVersion: state.PolicyVersion}
		metadata.Environment.Dir = state.Dir
		r.mu.Lock()
		r.boxes[state.ID] = box
		r.mu.Unlock()
	}
	value := core.Session{ID: key, Metadata: metadata}
	if err := r.store.Create(ctx, value); err != nil {
		if box != nil {
			r.mu.Lock()
			delete(r.boxes, metadata.Environment.ID)
			r.mu.Unlock()
		}
		return core.Session{}, err
	}
	return r.store.Load(ctx, key)
}

// Sandbox 仅供可信控制层管理默认环境；工具只能获得按调用绑定的 View。
// Sandbox exposes default environments to trusted controllers only; tools receive per-call views.
func (r *Runtime) Sandbox(environmentID string) (*sandbox.Sandbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.boxes[environmentID]
	if b == nil {
		return nil, ErrEnvironment
	}
	return b, nil
}

func (r *Runtime) resolve(ctx context.Context, b core.EnvironmentBinding) (core.EnvironmentBinding, Capabilities, error) {
	if r.environment != nil {
		return r.environment(ctx, b)
	}
	box, err := r.Sandbox(b.ID)
	if err != nil {
		return b, nil, err
	}
	state, err := box.State(ctx)
	if err != nil {
		return b, nil, err
	}
	b.Revision, b.PolicyVersion = state.Revision, state.PolicyVersion
	if state.Closed || state.Closing {
		return b, nil, core.ErrClosed
	}
	return b, func(_ context.Context, call core.Call) (core.ToolContext, error) {
		v := box.View(call)
		return core.ToolContext{Call: call, Files: v, Network: v, Resources: v}, nil
	}, nil
}

type Input struct {
	Content    []core.Content
	Parameters model.Parameters
	Model      core.ModelSelection
}

// Run 同步执行一个新 Turn；失败返回已生成的事实，不自动重放或覆盖冲突。
// Run synchronously executes a new turn; failures return accumulated facts without replay or conflict overwrites.
func (r *Runtime) Run(ctx context.Context, sessionID string, input Input) (turn core.Turn, err error) {
	data, err := json.Marshal(input)
	if err != nil {
		return turn, err
	}
	var copied Input
	if err = json.Unmarshal(data, &copied); err != nil {
		return turn, err
	}
	input = copied
	if err = input.Parameters.Validate(); err != nil {
		return turn, err
	}
	if len(input.Content) == 0 {
		return turn, ErrConfig
	}
	for _, c := range input.Content {
		if c.ToolCall != nil || c.ToolResult != nil {
			return turn, ErrConfig
		}
		switch c.Kind {
		case core.ContentText:
			if c.Artifact != nil {
				return turn, ErrConfig
			}
		case core.ContentImage, core.ContentAudio, core.ContentVideo, core.ContentFile:
			if c.Artifact == nil || c.Artifact.ID == "" || c.Text != "" {
				return turn, ErrConfig
			}
		default:
			return turn, ErrConfig
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	turnID := id.NewV7()
	r.mu.Lock()
	if _, ok := r.active[sessionID]; ok {
		r.mu.Unlock()
		return turn, session.ErrBusy
	}
	r.active[sessionID] = running{turnID, cancel}
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.active, sessionID); r.mu.Unlock() }()
	value, err := r.store.Load(ctx, sessionID)
	if err != nil {
		return turn, err
	}
	selection := value.Metadata.Model
	if input.Model.ID != "" {
		selection = input.Model
	}
	if value.Metadata.Agent != r.runner.Binding() || selection.ID == "" {
		return turn, ErrConfig
	}
	history := []core.Message{}
	compactions := []core.CompactionRecord{}
	for _, previous := range value.Turns {
		if previous.Status == core.TurnRunning || previous.Status == core.TurnWaitingApproval {
			return turn, session.ErrBusy
		}
		if previous.Kind == core.TurnNotification {
			continue
		}
		if !resolved(previous.Messages) {
			return turn, ErrHistory
		}
		history = append(history, previous.Messages...)
		compactions = append(compactions, previous.Compactions...)
	}
	binding, capabilities, err := r.resolve(ctx, value.Metadata.Environment)
	if err != nil {
		return turn, err
	}
	if capabilities == nil {
		return turn, ErrEnvironment
	}
	now := time.Now().UnixMilli()
	user := core.Message{ID: id.NewV7(), Role: core.RoleUser, Content: input.Content, CreatedAt: now}
	turn = core.Turn{ID: turnID, Kind: core.TurnInteraction, Metadata: core.TurnMetadata{Agent: r.runner.Binding(), Model: selection, Environment: binding}, Messages: []core.Message{user}, Status: core.TurnRunning, StartedAt: now}
	version, err := r.store.Commit(ctx, sessionID, session.Commit{OperationID: id.NewV7(), ExpectedVersion: value.Version, Turn: turn})
	if err != nil {
		return turn, err
	}
	apply := func(result agent.Result) {
		turn.Messages = append([]core.Message{user}, result.Messages...)
		turn.ModelCalls = result.ModelRecords
		turn.ToolCalls = result.ToolRecords
		turn.Contexts = result.Contexts
		turn.Compactions = result.Compactions
	}
	save := func(saveCtx context.Context) error {
		next, e := r.store.Commit(saveCtx, sessionID, session.Commit{OperationID: id.NewV7(), ExpectedVersion: version, Turn: turn})
		if e == nil {
			version = next
		}
		return e
	}
	// 取消后仍给予有界时间保存执行事实，不延续模型或工具操作。
	// Allow bounded recording after cancellation without continuing model or tool operations.
	record := func() error {
		saveCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		return save(saveCtx)
	}
	result, runErr := r.runner.Run(ctx, agent.Input{Compactions: compactions, Model: selection, Parameters: input.Parameters, Messages: append(history, user), Tools: core.ToolContext{Call: core.Call{SessionID: sessionID, TurnID: turnID, Dir: binding.Dir}}, Capabilities: capabilities, Checkpoint: func(_ context.Context, result agent.Result) error { apply(result); return record() }})
	apply(result)
	if errors.Is(runErr, agent.ErrCheckpoint) {
		return turn, runErr
	}
	ended := time.Now().UnixMilli()
	turn.EndedAt = &ended
	turn.Status = core.TurnCompleted
	if runErr != nil {
		turn.Status = core.TurnFailed
		turn.Error = &core.Failure{Code: "execution_failed", Message: "turn execution failed"}
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			turn.Status = core.TurnCancelled
			turn.Error.Code = "cancelled"
		}
	}
	if err = record(); err != nil {
		return turn, errors.Join(runErr, agent.ErrCheckpoint, err)
	}
	return turn, runErr
}

func resolved(messages []core.Message) bool {
	pending := map[string]bool{}
	for _, m := range messages {
		for _, c := range m.Content {
			if c.Kind == core.ContentToolCall && c.ToolCall != nil {
				pending[c.ToolCall.ID] = true
			}
			if c.Kind == core.ContentToolResult && c.ToolResult != nil {
				delete(pending, c.ToolResult.CallID)
			}
		}
	}
	return len(pending) == 0
}

// Cancel 只取消本实例拥有的执行；跨进程控制需要外部协调服务。
// Cancel targets execution owned by this instance; cross-process control needs external coordination.
func (r *Runtime) Cancel(sessionID, turnID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.active[sessionID]
	if !ok || a.turnID != turnID {
		return ErrNotRunning
	}
	a.cancel()
	return nil
}
