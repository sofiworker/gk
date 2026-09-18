// Package session 定义会话读取、创建与原子轮次提交契约。
// Package session defines session reads, creation and atomic turn commits.
package session

import (
	"context"
	"errors"

	"github.com/sofiworker/gk/gai/core"
)

var (
	ErrNotFound = errors.New("session not found")
	ErrConflict = errors.New("session version or operation conflict")
	ErrBusy     = errors.New("session has an active turn")
	ErrInvalid  = errors.New("invalid session mutation")
)

type Reader interface {
	Load(context.Context, string) (core.Session, error)
}
type Creator interface {
	Create(context.Context, core.Session) error
}

// Commit 同时追加消息并更新调用与状态；OperationID 相同的重发必须内容相同。
// Commit atomically appends messages and updates calls and status; retries sharing an OperationID must be identical.
type Commit struct {
	OperationID     string
	ExpectedVersion uint64
	Turn            core.Turn
}
type Writer interface {
	Commit(context.Context, string, Commit) (uint64, error)
}
type Store interface {
	Reader
	Creator
	Writer
}
