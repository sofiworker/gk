package core

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
)

var (
	ErrPath          = errors.New("invalid sandbox path")
	ErrDenied        = errors.New("sandbox access denied")
	ErrUnsupported   = errors.New("sandbox operation unsupported")
	ErrQuota         = errors.New("sandbox quota exceeded")
	ErrConflict      = errors.New("sandbox version conflict")
	ErrSyncConflict  = errors.New("sandbox synchronization conflict")
	ErrClosed        = errors.New("sandbox closed")
	ErrBusy          = errors.New("sandbox busy")
	ErrCursorExpired = errors.New("sandbox audit cursor expired")
	ErrUnknown       = errors.New("sandbox external result unknown")
	ErrNotExist      = fs.ErrNotExist
	ErrExist         = fs.ErrExist
)

type Access uint8

const (
	Read Access = 1 << iota
	Write
	ReadWrite = Read | Write
)

type SyncMode string

const (
	SyncNone      SyncMode = "none"
	SyncExplicit  SyncMode = "explicit"
	SyncImmediate SyncMode = "immediate"
)

type SyncState string

const (
	SyncClean    SyncState = "clean"
	SyncPending  SyncState = "pending"
	SyncConflict SyncState = "conflict"
	SyncFailed   SyncState = "failed"
)

// Call 的 ID 用于文件修改幂等；Dir 只影响本次相对路径解析。
// Call.ID provides mutation idempotency; Dir affects only this call's relative paths.
type Call struct {
	ID         string
	Dir        string
	SessionID  string
	TurnID     string
	ToolCallID string
}
type Result struct {
	OperationID  string
	Revision     uint64
	LocalApplied bool
	SyncState    SyncState
}
type Entry struct {
	Path      string
	Dir       bool
	Size      int64
	ContentID string
}

// Node 的 Data 在接口边界复制，目录不携带文件内容。
// Node.Data is copied across interface boundaries; directories carry no file content.
type Node struct {
	Dir  bool
	Data []byte
}
type Tree map[string]Node

type FileReader interface {
	ReadFile(context.Context, Call, string) ([]byte, error)
	ReadDir(context.Context, Call, string) ([]Entry, error)
	Stat(context.Context, Call, string) (Entry, error)
}
type FileWriter interface {
	WriteFile(context.Context, Call, string, []byte) (Result, error)
	Mkdir(context.Context, Call, string) (Result, error)
	Remove(context.Context, Call, string) (Result, error)
	Rename(context.Context, Call, string, string) (Result, error)
}
type Files interface {
	FileReader
	FileWriter
}
type Attribution struct{ SessionID, TurnID, ToolCallID string }
type FileChange struct {
	Path          string
	Before, After *Entry
}
type Event struct {
	Sequence      uint64
	SandboxID     string
	OperationID   string
	Attribution   Attribution
	Kind          string
	Path          string
	Destination   string
	StartedAt     int64
	EndedAt       int64
	Before        uint64
	After         uint64
	PolicyVersion uint64
	BeforeContent string
	AfterContent  string
	Error         string
	Changes       []FileChange
	StatusCode    int
	RequestBytes  int64
	ResponseBytes int64
}
type Auditor interface {
	Events(context.Context, uint64, int) ([]Event, error)
	Content(context.Context, string) ([]byte, error)
}
type Snapshot struct {
	ID            string
	Revision      uint64
	PolicyVersion uint64
}
type ChangeSet struct {
	From    uint64
	To      uint64
	Changes []FileChange
}
type State struct {
	ID            string
	Revision      uint64
	PolicyVersion uint64
	Dir           string
	Closing       bool
	Closed        bool
	SyncPaused    bool
}
type Snapshots interface {
	Snapshot(context.Context) (Snapshot, error)
	Restore(context.Context, string) (Result, error)
	Release(context.Context, string) error
}

// Change 中路径相对于绑定根；Before/After 为 nil 分别表示创建/删除。
// Change paths are relative to the binding root; nil Before/After mean creation/deletion.
type Change struct {
	Path          string
	Before, After *Node
}
type Batch struct {
	ID         string
	SandboxID  string
	ResourceID string
	Revision   uint64
	Changes    []Change
}

// Target 必须有序应用并返回已成功的前缀长度；失败不能隐瞒已应用的修改。
// Target must apply in order and return the successful prefix length, including on failure.
// 同一外部目标须复用同一个协调实现，调用方保证宿主没有未协调写入。
// A shared external target must use one coordinator; callers exclude uncoordinated host writes.
type Target interface {
	Apply(context.Context, Batch) (int, error)
}
type ResourceBinding struct {
	ID     string
	Path   string
	Access Access
	Mode   SyncMode
	Target Target
}
type Resource struct {
	Binding ResourceBinding
	Tree    Tree
}
type SyncStatus struct {
	ResourceID string
	Revision   uint64
	State      SyncState
	BatchID    string
	Applied    int
	Total      int
	Error      string
}
type Syncer interface {
	Sync(context.Context, string) (SyncStatus, error)
}

type HTTPRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}
type HTTPResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// HTTPSender 是可信传输边界，必须执行目标、跳转、地址、配额和凭据策略。
// HTTPSender is a trusted boundary enforcing destination, redirect, address, quota and credential policy.
type HTTPSender interface {
	Send(context.Context, HTTPRequest) (HTTPResponse, error)
}
type Network interface {
	Request(context.Context, Call, HTTPRequest) (HTTPResponse, error)
}

type RequestState string

const (
	Requested     RequestState = "requested"
	Approved      RequestState = "approved"
	Denied        RequestState = "denied"
	Providing     RequestState = "providing"
	Available     RequestState = "available"
	ProvideFailed RequestState = "failed"
)

type ResourceRequest struct {
	ID          string
	Description string
	Access      Access
	State       RequestState
	ResourceID  string
	Path        string
	Error       string
}
type Requester interface {
	RequestResource(context.Context, string, string, Access) (ResourceRequest, error)
}
