// Package ghttpotel 把 gotel 的 Tracer/Meter 抽象适配为 ghttp 中间件。
// Package ghttpotel adapts the gotel Tracer/Meter abstractions into ghttp middleware.
//
// 包名使用 ghttpotel 而非 gotel，避免与被导入的 gotel 包同名冲突；
// 目录仍为 ghttp/adapters/gotel，符合模块依赖策略（能力层之间只经适配层拼接）。
// The package is named ghttpotel instead of gotel to avoid clashing with the imported gotel
// package; the directory stays ghttp/adapters/gotel per the module dependency policy.
package ghttpotel
