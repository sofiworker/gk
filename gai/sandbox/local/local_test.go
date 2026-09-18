//go:build unix

package local_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	s "github.com/sofiworker/gk/gai/sandbox"
	"github.com/sofiworker/gk/gai/sandbox/local"
	"github.com/sofiworker/gk/gai/sandbox/memory"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func TestImportSyncConflictAndCompensation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a"), []byte("base"), 0600))
	tree, err := local.Import(ctx, dir, local.Limits{Bytes: 1024, Entries: 20})
	must(t, err)
	target, err := local.Open(dir)
	must(t, err)
	t.Cleanup(func() { must(t, target.Close()) })
	b, err := memory.New()
	must(t, err)
	must(t, b.Provide(ctx, s.Resource{Binding: s.Binding{ID: "project", Path: "/chosen", Access: s.ReadWrite, Mode: s.SyncExplicit, Target: target}, Tree: tree}))
	snap, err := b.Snapshot(ctx)
	must(t, err)
	_, err = b.WriteFile(ctx, s.Call{}, "/chosen/a", []byte("agent"))
	must(t, err)
	before, err := os.ReadFile(filepath.Join(dir, "a"))
	must(t, err)
	if string(before) != "base" {
		t.Fatal("explicit sync wrote early")
	}
	must(t, os.WriteFile(filepath.Join(dir, "a"), []byte("human"), 0600))
	status, err := b.Sync(ctx, "project")
	if !errors.Is(err, s.ErrSyncConflict) || status.Applied != 0 {
		t.Fatal(status, err)
	}
	host, err := local.Import(ctx, dir, local.Limits{Bytes: 1024, Entries: 20})
	must(t, err)
	must(t, b.Rebase(ctx, "project", host))
	_, err = b.Sync(ctx, "project")
	must(t, err)
	data, err := os.ReadFile(filepath.Join(dir, "a"))
	must(t, err)
	if string(data) != "agent" {
		t.Fatal(string(data))
	}
	_, err = b.Restore(ctx, snap.ID)
	must(t, err)
	data, err = os.ReadFile(filepath.Join(dir, "a"))
	must(t, err)
	if string(data) != "agent" {
		t.Fatal("restore wrote host")
	}
	_, err = b.Sync(ctx, "project")
	must(t, err)
	data, err = os.ReadFile(filepath.Join(dir, "a"))
	must(t, err)
	if string(data) != "base" {
		t.Fatal(string(data))
	}
	_, err = b.Mkdir(ctx, s.Call{}, "/chosen/nested")
	must(t, err)
	_, err = b.Rename(ctx, s.Call{}, "/chosen/a", "/chosen/nested/b")
	must(t, err)
	_, err = b.Sync(ctx, "project")
	must(t, err)
	if _, err = os.Stat(filepath.Join(dir, "a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	_, err = b.Remove(ctx, s.Call{}, "/chosen/nested")
	must(t, err)
	_, err = b.Sync(ctx, "project")
	must(t, err)
	if _, err = os.Stat(filepath.Join(dir, "nested")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
func TestLocalRejectsLinksAndEscape(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	outside := t.TempDir()
	must(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("keep"), 0600))
	must(t, os.Symlink(outside, filepath.Join(dir, "link")))
	if _, err := local.Import(ctx, dir, local.Limits{Bytes: 1000, Entries: 20}); !errors.Is(err, s.ErrUnsupported) {
		t.Fatal(err)
	}
	target, err := local.Open(dir)
	must(t, err)
	defer func() { must(t, target.Close()) }()
	for _, p := range []string{"../secret", "/secret", "link/secret"} {
		_, err = target.Apply(ctx, s.Batch{Changes: []s.Change{{Path: p, After: &s.Node{Data: []byte("bad")}}}})
		if err == nil {
			t.Fatal("escape accepted", p)
		}
	}
	data, err := os.ReadFile(filepath.Join(outside, "secret"))
	must(t, err)
	if string(data) != "keep" {
		t.Fatal(string(data))
	}
	must(t, os.Remove(filepath.Join(dir, "link")))
	must(t, os.WriteFile(filepath.Join(dir, "file"), []byte("hello"), 0600))
	must(t, os.Link(filepath.Join(dir, "file"), filepath.Join(dir, "alias")))
	if _, err = local.Import(ctx, dir, local.Limits{Bytes: 1000, Entries: 20}); !errors.Is(err, s.ErrUnsupported) {
		t.Fatal(err)
	}
}
func TestLocalLimitsPartialProgressAndClose(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "a"), []byte("large"), 0600))
	for _, lim := range []local.Limits{{Bytes: 2, Entries: 20}, {Bytes: 100, Entries: 1}, {}} {
		if _, err := local.Import(ctx, dir, lim); !errors.Is(err, s.ErrQuota) {
			t.Fatal(err)
		}
	}
	target, err := local.Open(dir)
	must(t, err)
	n, err := target.Apply(ctx, s.Batch{Changes: []s.Change{{Path: "b", After: &s.Node{Data: []byte("new")}}, {Path: "a", Before: &s.Node{Data: []byte("wrong")}, After: &s.Node{Data: []byte("overwrite")}}}})
	if n != 1 || !errors.Is(err, s.ErrSyncConflict) {
		t.Fatal(n, err)
	}
	must(t, target.Close())
	must(t, target.Close())
	_, err = target.Apply(ctx, s.Batch{})
	if !errors.Is(err, s.ErrClosed) {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = local.Import(cancelled, dir, local.Limits{Bytes: 100, Entries: 20}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestTemporaryNameDoesNotAliasDestination(t *testing.T) {
	dir := t.TempDir()
	target, err := local.Open(dir)
	must(t, err)
	defer func() { must(t, target.Close()) }()
	n, err := target.Apply(context.Background(), s.Batch{Changes: []s.Change{{Path: ".gai-sync-1", After: &s.Node{Data: []byte("retained")}}}})
	must(t, err)
	if n != 1 {
		t.Fatal(n)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gai-sync-1"))
	must(t, err)
	if string(data) != "retained" {
		t.Fatal(string(data))
	}
}
