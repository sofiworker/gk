package app

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestStoreCRUDAndReset(t *testing.T) {
	store := NewStore()
	created, err := store.Create("tenant-a", "2", "second")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Version != 1 || created.UpdatedAt.IsZero() {
		t.Fatalf("Create() = %+v, want version 1 and timestamp", created)
	}
	if _, err := store.Create("tenant-a", "2", "duplicate"); !errors.Is(err, ErrItemExists) {
		t.Fatalf("duplicate Create() error = %v, want ErrItemExists", err)
	}

	got, ok := store.Get("tenant-a", "2")
	if !ok || got != created {
		t.Fatalf("Get() = (%+v, %v), want (%+v, true)", got, ok, created)
	}
	if _, ok := store.Get("tenant-b", "2"); ok {
		t.Fatal("Get() crossed namespace boundary")
	}

	if _, err := store.Create("tenant-a", "1", "first"); err != nil {
		t.Fatalf("Create(second item) error = %v", err)
	}
	list := store.List("tenant-a")
	if len(list) != 2 {
		t.Fatalf("List() length = %d, want 2", len(list))
	}
	if ids := []string{list[0].ID, list[1].ID}; !reflect.DeepEqual(ids, []string{"1", "2"}) {
		t.Fatalf("List() IDs = %v, want stable ID order", ids)
	}

	if _, err := store.Update("tenant-a", "missing", "name", 1); !errors.Is(err, errItemNotFound) {
		t.Fatalf("Update(missing) error = %v, want errItemNotFound", err)
	}
	if _, err := store.Update("tenant-a", "2", "wrong", 9); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("Update(version conflict) error = %v, want ErrVersionConflict", err)
	}
	updated, err := store.Update("tenant-a", "2", "updated", created.Version)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "updated" || updated.Version != 2 || updated.UpdatedAt.Before(created.UpdatedAt) {
		t.Fatalf("Update() = %+v, want updated name, version 2, nondecreasing timestamp", updated)
	}
	if store.Delete("tenant-b", "2") || !store.Delete("tenant-a", "2") || store.Delete("tenant-a", "2") {
		t.Fatal("Delete() namespace/existence semantics are incorrect")
	}

	store.Reset()
	if got := store.List("tenant-a"); len(got) != 0 {
		t.Fatalf("List() after Reset() = %v, want empty", got)
	}
}

func TestStoreConcurrentNamespaceIsolation(t *testing.T) {
	store := NewStore()
	const workers = 32
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			namespace := fmt.Sprintf("tenant-%02d", worker)
			for item := 0; item < 20; item++ {
				id := fmt.Sprintf("item-%02d", item)
				created, err := store.Create(namespace, id, namespace)
				if err != nil {
					t.Errorf("Create(%q, %q) error = %v", namespace, id, err)
					return
				}
				if _, err := store.Update(namespace, id, "updated", created.Version); err != nil {
					t.Errorf("Update(%q, %q) error = %v", namespace, id, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	for worker := 0; worker < workers; worker++ {
		namespace := fmt.Sprintf("tenant-%02d", worker)
		items := store.List(namespace)
		if len(items) != 20 {
			t.Fatalf("List(%q) length = %d, want 20", namespace, len(items))
		}
		for _, item := range items {
			if item.Namespace != namespace || item.Name != "updated" || item.Version != 2 {
				t.Fatalf("List(%q) contains %+v", namespace, item)
			}
		}
	}
}
