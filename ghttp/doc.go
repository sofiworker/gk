// Package ghttp 是一个基于标准库 net/http 的、面向 Go 泛型的 typed HTTP 路由框架。
// Package ghttp is a generics-oriented, typed HTTP routing framework built on
// the standard net/http package.
//
// 入口按输入组合分函数（GetParams / PostBody / PostParamsBody 等），handler 收裸
// 参数，类型全推断，无包裹容器。params 经 struct tag（path:/query:/header:）绑定，
// 请求体经 RequestDecoder 解码，输出经 OutputSpec 编码；需完全接管响应时用 RawHandle。
// Entries are split by input shape (GetParams / PostBody / PostParamsBody, etc.);
// handlers take naked parameters with all type parameters inferred and no wrapper
// container. Params bind via struct tags (path:/query:/header:), the body is
// decoded by a RequestDecoder, and output is encoded by an OutputSpec; use
// RawHandle to fully own the response.
//
// 核心取向:单一 typed 执行模型 + 显式 RawHandler 逃生;纯 net/http 地基,
// 不引入 fasthttp 或自管 TCP;性能红利只来自池化上下文与零反射 codec。
// Core stance: a single typed execution model plus an explicit RawHandler escape
// hatch; a pure net/http foundation with no fasthttp or self-managed TCP;
// performance comes only from pooled contexts and zero-reflection codecs.
package ghttp
