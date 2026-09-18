// Package local 提供显式宿主目录导入与写回；调用方须保证导入一致性及写回期间独占。
// Package local provides explicit host import and synchronization; callers ensure consistent imports and exclusive host writes.
package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	s "github.com/sofiworker/gk/gai/sandbox"
)

type Limits struct {
	Bytes   int64
	Entries int
}

// Import 不跟随符号链接，不接受设备、管道或多链接文件。
// Import rejects symlinks, devices, pipes and multiply linked files.
func Import(ctx context.Context, dir string, limits Limits) (s.Tree, error) {
	if !supportedPlatform() {
		return nil, s.ErrUnsupported
	}
	if limits.Bytes <= 0 || limits.Entries < 1 {
		return nil, s.ErrQuota
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	tree := s.Tree{}
	used := int64(0)
	var walk func(string) error
	walk = func(p string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(tree) >= limits.Entries {
			return s.ErrQuota
		}
		info, err := root.Lstat(p)
		if err != nil {
			return err
		}
		if err = checkType(info); err != nil {
			return err
		}
		if info.IsDir() {
			tree[p] = s.Node{Dir: true}
			f, err := root.Open(p)
			if err != nil {
				return err
			}
			entries, err := f.ReadDir(limits.Entries - len(tree) + 1)
			_ = f.Close()
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			if len(entries) > limits.Entries-len(tree) {
				return s.ErrQuota
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
			for _, e := range entries {
				if err = walk(path.Join(p, e.Name())); err != nil {
					return err
				}
			}
			return nil
		}
		if info.Size() > limits.Bytes-used {
			return s.ErrQuota
		}
		f, err := root.Open(p)
		if err != nil {
			return err
		}
		current, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return err
		}
		if !os.SameFile(info, current) {
			_ = f.Close()
			return s.ErrConflict
		}
		data, err := io.ReadAll(io.LimitReader(f, limits.Bytes-used+1))
		_ = f.Close()
		if err != nil {
			return err
		}
		used += int64(len(data))
		if used > limits.Bytes {
			return s.ErrQuota
		}
		tree[p] = s.Node{Data: data}
		return nil
	}
	if err = walk("."); err != nil {
		return nil, err
	}
	return tree, nil
}
func checkType(info fs.FileInfo) error {
	if !info.IsDir() && !info.Mode().IsRegular() {
		return s.ErrUnsupported
	}
	if info.Mode().IsRegular() && multipleLinks(info) {
		return s.ErrUnsupported
	}
	return nil
}

// Target 的根句柄约束路径；共享目录必须共享 Target，且排除其他宿主写入者。
// Target confines paths through a root handle; shared directories must share Target and exclude other host writers.
type Target struct {
	gate    chan struct{}
	root    *os.Root
	closed  bool
	counter uint64
}

func Open(dir string) (*Target, error) {
	if !supportedPlatform() {
		return nil, s.ErrUnsupported
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Target{root: root, gate: make(chan struct{}, 1)}, nil
}
func (t *Target) Close() error {
	t.gate <- struct{}{}
	defer func() { <-t.gate }()
	if t.closed {
		return nil
	}
	t.closed = true
	return t.root.Close()
}
func valid(p string) bool {
	return p != "." && p != "" && path.Clean(p) == p && !strings.HasPrefix(p, "/") && p != ".." && !strings.HasPrefix(p, "../") && !strings.ContainsAny(p, "\\\x00")
}
func (t *Target) inspect(p string, maxBytes int64) (*s.Node, error) {
	parts := strings.Split(p, "/")
	for i := range parts {
		prefix := strings.Join(parts[:i+1], "/")
		info, err := t.root.Lstat(prefix)
		if errors.Is(err, fs.ErrNotExist) && i == len(parts)-1 {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if err = checkType(info); err != nil {
			return nil, err
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, s.ErrSyncConflict
		}
		if i == len(parts)-1 {
			if info.IsDir() {
				return &s.Node{Dir: true}, nil
			}
			if info.Size() > maxBytes {
				return nil, s.ErrSyncConflict
			}
			f, err := t.root.Open(p)
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
			_ = f.Close()
			if err != nil {
				return nil, err
			}
			if int64(len(data)) > maxBytes {
				return nil, s.ErrSyncConflict
			}
			return &s.Node{Data: data}, nil
		}
	}
	return nil, s.ErrPath
}
func equal(a, b *s.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Dir == b.Dir && bytes.Equal(a.Data, b.Data)
}
func (t *Target) Apply(ctx context.Context, batch s.Batch) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	select {
	case t.gate <- struct{}{}:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	defer func() { <-t.gate }()
	if t.closed {
		return 0, s.ErrClosed
	}
	for i, c := range batch.Changes {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		if !valid(c.Path) {
			return i, s.ErrPath
		}
		maxBytes := int64(0)
		if c.Before != nil {
			maxBytes = int64(len(c.Before.Data))
		}
		actual, err := t.inspect(c.Path, maxBytes)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				err = s.ErrSyncConflict
			}
			return i, fmt.Errorf("inspect %s: %w", c.Path, err)
		}
		if !equal(actual, c.Before) {
			return i, fmt.Errorf("compare %s: %w", c.Path, s.ErrSyncConflict)
		}
		if c.After == nil {
			err = t.root.Remove(c.Path)
		} else if c.After.Dir {
			if actual == nil {
				err = t.root.Mkdir(c.Path, 0700)
			} else if !actual.Dir {
				err = s.ErrSyncConflict
			}
		} else {
			err = t.write(c.Path, c.After.Data)
		}
		if err != nil {
			return i, fmt.Errorf("apply %s: %w", c.Path, err)
		}
	}
	return len(batch.Changes), nil
}
func (t *Target) write(p string, data []byte) error {
	var f *os.File
	var err error
	name := ""
	for attempts := 0; attempts < 100; attempts++ {
		t.counter++
		name = path.Join(path.Dir(p), fmt.Sprintf(".gai-sync-%d", t.counter))
		if name == p {
			continue
		}
		f, err = t.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if !errors.Is(err, fs.ErrExist) {
			break
		}
	}
	if err != nil {
		return err
	}
	defer func() { _ = t.root.Remove(name) }()
	n, err := f.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return t.root.Rename(name, p)
}
