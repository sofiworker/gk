//go:build ignore

// 新 SDK 类型化输出设计示例；对应接口尚未实现，暂不参与构建。
// Typed output design examples for the new SDK; excluded until the APIs are implemented.
package examples

import (
	"context"
	"fmt"

	"github.com/sofiworker/gk/gai/agent"
	"github.com/sofiworker/gk/gai/model"
	"github.com/sofiworker/gk/gai/output"
)

type Person struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// RunStructuredOutput 从模型依赖创建 Agent，并使用验证后的结构体字段。
// RunStructuredOutput creates an Agent from a model dependency and uses validated fields.
func RunStructuredOutput(ctx context.Context, chatModel model.Model) (string, error) {
	a, err := agent.New(agent.WithModel(chatModel))
	if err != nil {
		return "", err
	}
	var person Person
	_, err = a.Run(ctx, agent.Textf(
		"生成一个虚构人物，严格按照 {} 返回，不要 Markdown 代码围栏",
		output.Bind(&person),
	))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("姓名：%s，年龄：%d", person.Name, person.Age), nil
}

// ReflectedSchema 只从值的类型生成 Schema，不发送字段值，也不回填该值。
// ReflectedSchema derives a schema from the value's type without sending field values or binding it.
func ReflectedSchema(ctx context.Context, a *agent.Agent) (string, error) {
	result, err := a.Run(ctx, agent.Textf(
		"生成一个虚构人物，严格按照 {} 返回",
		output.JSONType(Person{}),
	))
	if err != nil {
		return "", err
	}
	return result.Text(), nil
}

// ReflectedShapes 展示常见的非泛型类型形状；JSONType 只读取 reflect.Type。
// ReflectedShapes demonstrates common non-generic shapes; JSONType reads only reflect.Type.
func ReflectedShapes() {
	output.JSONType(Person{})
	output.JSONType(&Person{})
	output.JSONType([]Person{})
	output.JSONType([]Person(nil))
	output.JSONType([2]Person{})
	output.JSONType([0]Person{})
	output.JSONType(map[string]Person{})
	output.JSONType((*Person)(nil)) // 类型占位，无需分配对象；Type token without allocating an instance.
	output.JSONType([]*Person(nil))
	output.JSONType(map[string]any(nil))
	output.JSONType(struct{}{}) // 空对象类型，不代表任意 JSON；Empty object type, not arbitrary JSON.
	output.JSONType((*struct{})(nil))
	output.JSONType(any(Person{})) // 保留动态类型 Person；Retains the dynamic Person type.
	// output.JSONType(nil) 没有类型，运行前报错。
	// output.JSONType(nil) has no type and fails before dispatch.
}

// PointerSchema 使用 typed nil 生成提示词约束，返回原始文本而不绑定对象。
// PointerSchema uses a typed nil for the prompt contract and returns text without binding.
func PointerSchema(ctx context.Context, a *agent.Agent) (string, error) {
	result, err := a.Run(ctx, agent.Textf(
		"生成一个虚构人物，严格按照 {} 返回",
		output.JSONType((*Person)(nil)),
	))
	if err != nil {
		return "", err
	}
	return result.Text(), nil
}

// ReflectedBinding 接收运行时类型的指针；nil 或不可写目标在模型请求前报错。
// ReflectedBinding accepts a runtime-typed pointer; nil or unwritable targets fail before model dispatch.
func ReflectedBinding(ctx context.Context, a *agent.Agent) (Person, error) {
	var person Person
	_, err := a.Run(ctx, agent.Textf(
		"生成一个虚构人物，严格按照 {} 返回",
		output.Bind(&person),
	))
	return person, err
}

// BindPointer 可写外层指针允许 SDK 在成功时分配内层目标。
// BindPointer uses a writable outer pointer so the SDK can allocate the target on success.
func BindPointer(ctx context.Context, a *agent.Agent) (*Person, error) {
	var person *Person
	_, err := a.Run(ctx, agent.Textf("生成一个虚构人物，按照 {} 返回", output.Bind(&person)))
	return person, err
}

// BindSlice 的数量来自提示词；切片类型本身不限制长度。
// BindSlice requests a count in the prompt; the slice type does not constrain length.
func BindSlice(ctx context.Context, a *agent.Agent) ([]Person, error) {
	var people []Person
	_, err := a.Run(ctx, agent.Textf("生成三个虚构人物，按照 {} 返回", output.Bind(&people)))
	return people, err
}

// BindArray 的 Schema 要求恰好两个元素，避免解码时截断或补零。
// BindArray requires exactly two elements in the schema to avoid truncation or zero padding.
func BindArray(ctx context.Context, a *agent.Agent) ([2]Person, error) {
	var people [2]Person
	_, err := a.Run(ctx, agent.Textf("生成两个虚构人物，按照 {} 返回", output.Bind(&people)))
	return people, err
}

// BindMap 允许动态键，值仍按 Person 验证；无需预先 make。
// BindMap allows dynamic keys with validated Person values; no initial make is needed.
func BindMap(ctx context.Context, a *agent.Agent) (map[string]Person, error) {
	var people map[string]Person
	_, err := a.Run(ctx, agent.Textf("生成以人物编号为键的虚构人物字典，按照 {} 返回", output.Bind(&people)))
	return people, err
}

type Household struct {
	Owner   Person            `json:"owner"`
	Partner *Person           `json:"partner,omitempty"`
	Members []*Person         `json:"members"`
	Extra   map[string]string `json:"extra,omitempty"`
}

// BindNested 的内部指针允许 null；omitempty 允许对应字段缺省。
// BindNested allows null for internal pointers and omission for omitempty fields.
func BindNested(ctx context.Context, a *agent.Agent) (Household, error) {
	var household Household
	_, err := a.Run(ctx, agent.Textf("生成一个虚构家庭，按照 {} 返回", output.Bind(&household)))
	return household, err
}

// BindDynamicTarget 即使通过 any 传递，目标仍保留具体的可写指针类型。
// BindDynamicTarget retains a concrete writable pointer even when passed through any.
func BindDynamicTarget(ctx context.Context, a *agent.Agent) (Person, error) {
	var person Person
	var target any = &person
	_, err := a.Run(ctx, agent.Textf("生成一个虚构人物，按照 {} 返回", output.Bind(target)))
	return person, err
}

// BindEmptyObject 只接受 {}；struct{} 不是任意对象的占位符。
// BindEmptyObject accepts only {}; struct{} is not a placeholder for arbitrary objects.
func BindEmptyObject(ctx context.Context, a *agent.Agent) error {
	var empty struct{}
	_, err := a.Run(ctx, agent.Textf("返回符合 {} 的空对象", output.Bind(&empty)))
	return err
}

// BindOpenObject 允许未知字段及任意 JSON 值，数字按标准 JSON 解码规则处理。
// BindOpenObject allows unknown fields and arbitrary JSON values with standard JSON number decoding.
func BindOpenObject(ctx context.Context, a *agent.Agent) (map[string]any, error) {
	var object map[string]any
	_, err := a.Run(ctx, agent.Textf("生成一个虚构人物，字段自由，按照 {} 返回", output.Bind(&object)))
	return object, err
}

// BindingFailurePreservesTarget 在失败时返回原值；成功时返回完整的新值。
// BindingFailurePreservesTarget returns the original value on failure and the full new value on success.
func BindingFailurePreservesTarget(ctx context.Context, a *agent.Agent) (Person, error) {
	person := Person{Name: "原值", Age: 20}
	_, err := a.Run(ctx, agent.Textf("生成一个虚构人物，按照 {} 返回", output.Bind(&person)))
	return person, err
}

// InvalidOutputExamples 中每次 Run 都应在请求模型前失败，不发送请求。
// Each Run in InvalidOutputExamples must fail before dispatch without calling the model.
func InvalidOutputExamples(ctx context.Context, a *agent.Agent) map[string]error {
	cases := []struct {
		name string
		spec output.Spec
	}{
		{"schema without type", output.JSONType(nil)},
		{"bind without target", output.Bind(nil)},
		{"bind nil pointer", output.Bind((*Person)(nil))},
		{"bind non-pointer", output.Bind(Person{})},
		{"unsupported map key", output.JSONType(map[int]Person{})},
		{"unsupported function", output.JSONType((func())(nil))},
	}
	results := make(map[string]error, len(cases))
	for _, tc := range cases {
		_, err := a.Run(ctx, agent.Textf("按照 {} 返回", tc.spec))
		results[tc.name] = err
	}
	return results
}
