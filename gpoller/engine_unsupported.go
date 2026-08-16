//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package gpoller

// NewEngine 在当前平台返回 ErrNotSupported。
// 设计上 Windows 不自研 readiness 层（reactor 走标准库 netpoller 路径，
// 见 docs/superpowers/specs/2026-08-16-gnet-network-foundation-redesign.md §7）。
//
// NewEngine returns ErrNotSupported on this platform. By design Windows has no
// self-built readiness layer: the reactor uses the stdlib netpoller path there
// (see the gnet redesign spec, §7).
func NewEngine(opts ...EngineOption) (EventEngine, error) {
	_ = opts
	return nil, ErrNotSupported
}
