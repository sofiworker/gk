//go:build !linux

package gpoller

// NewCompletionEngine 在无 io_uring 的平台返回 ErrNotSupported。
// NewCompletionEngine returns ErrNotSupported on platforms without io_uring.
func NewCompletionEngine() (CompletionEngine, error) {
	return nil, ErrNotSupported
}
