// Package sandbox 提供环境契约的领域入口；权威共享类型定义于 core。
// Package sandbox exposes environment contracts whose authoritative shared types live in core.
package sandbox

import "github.com/sofiworker/gk/gai/core"

type Access = core.Access
type SyncMode = core.SyncMode
type SyncState = core.SyncState
type Call = core.Call
type Result = core.Result
type Entry = core.Entry
type Node = core.Node
type Tree = core.Tree
type FileReader = core.FileReader
type FileWriter = core.FileWriter
type Files = core.Files
type Attribution = core.Attribution
type FileChange = core.FileChange
type Event = core.Event
type Auditor = core.Auditor
type Snapshot = core.Snapshot
type ChangeSet = core.ChangeSet
type State = core.State
type Snapshots = core.Snapshots
type Change = core.Change
type Batch = core.Batch
type Target = core.Target
type Binding = core.ResourceBinding
type Resource = core.Resource
type SyncStatus = core.SyncStatus
type Syncer = core.Syncer
type HTTPRequest = core.HTTPRequest
type HTTPResponse = core.HTTPResponse
type HTTPSender = core.HTTPSender
type Network = core.Network
type RequestState = core.RequestState
type ResourceRequest = core.ResourceRequest
type Requester = core.Requester

const (
	Read          = core.Read
	Write         = core.Write
	ReadWrite     = core.ReadWrite
	SyncNone      = core.SyncNone
	SyncExplicit  = core.SyncExplicit
	SyncImmediate = core.SyncImmediate
	SyncClean     = core.SyncClean
	SyncPending   = core.SyncPending
	SyncConflict  = core.SyncConflict
	SyncFailed    = core.SyncFailed
	Requested     = core.Requested
	Approved      = core.Approved
	Denied        = core.Denied
	Providing     = core.Providing
	Available     = core.Available
	ProvideFailed = core.ProvideFailed
)

var (
	ErrPath          = core.ErrPath
	ErrDenied        = core.ErrDenied
	ErrUnsupported   = core.ErrUnsupported
	ErrQuota         = core.ErrQuota
	ErrConflict      = core.ErrConflict
	ErrSyncConflict  = core.ErrSyncConflict
	ErrClosed        = core.ErrClosed
	ErrBusy          = core.ErrBusy
	ErrCursorExpired = core.ErrCursorExpired
	ErrUnknown       = core.ErrUnknown
	ErrNotExist      = core.ErrNotExist
	ErrExist         = core.ErrExist
)
