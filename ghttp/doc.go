// Package ghttp 是一个基于标准库 net/http 的、面向 Go 泛型的 typed HTTP 路由框架。
// Package ghttp is a generics-oriented, typed HTTP routing framework built on
// the standard net/http package.
//
// 本包正在从零重写,设计见:
// This package is being rewritten from scratch; see the design spec:
// docs/superpowers/specs/2026-08-19-ghttp-internal-new-typed-rewrite.md
//
// 核心取向:单一 typed 执行模型 + 显式 RawHandler 逃生;纯 net/http 地基,
// 不引入 fasthttp 或自管 TCP;性能红利只来自池化上下文与零反射 codec。
// Core stance: a single typed execution model plus an explicit RawHandler
// escape hatch; a pure net/http foundation with no fasthttp or self-managed
// TCP; performance comes only from pooled contexts and zero-reflection codecs.
package ghttp
