package grx

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

type embeddedSample struct {
	CreatedAt time.Time `json:"created_at"`
}

type sampleStruct struct {
	embeddedSample
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
}

func (s *sampleStruct) SetName(name string) {
	s.Name = name
}

func (s sampleStruct) FullName(prefix string) string {
	return prefix + s.Name
}

func (s *sampleStruct) Sum(a, b int) int {
	return a + b
}

func TestFieldLookupAndTags(t *testing.T) {
	defaultFieldCache.ClearCache()

	tpe := reflect.TypeOf(sampleStruct{})

	info, ok := LookupFieldInfo(tpe, "Name")
	if !ok {
		t.Fatalf("LookupFieldInfo failed")
	}
	if info.Field.Name != "Name" {
		t.Fatalf("unexpected field name %s", info.Field.Name)
	}

	tagInfo, ok := LookupFieldByTag(tpe, "json", "name")
	if !ok {
		t.Fatalf("LookupFieldByTag failed")
	}
	if tagInfo.Field.Name != "Name" {
		t.Fatalf("tag lookup returned %s", tagInfo.Field.Name)
	}

	fields := Fields(tpe)
	if len(fields) == 0 {
		t.Fatalf("Fields returned empty slice")
	}

	val := sampleStruct{ID: 42, Name: "John"}
	if v, ok := GetFieldValue(reflect.ValueOf(val), "Name"); !ok || v.String() != "John" {
		t.Fatalf("GetFieldValue on struct failed: %v %v", ok, v)
	}

	if v, ok := GetFieldValue(reflect.ValueOf(&val), "ID"); !ok || v.Int() != 42 {
		t.Fatalf("GetFieldValue on pointer failed: %v %v", ok, v)
	}
}

func TestMethodLookupAndCall(t *testing.T) {
	defaultFieldCache.ClearCache()

	s := &sampleStruct{Name: "Alice"}

	methods := Methods(reflect.TypeOf(s))
	if len(methods) == 0 {
		t.Fatalf("Methods returned empty slice")
	}

	if _, err := CallMethod(s, "SetName", "Bob"); err != nil {
		t.Fatalf("CallMethod SetName failed: %v", err)
	}
	if s.Name != "Bob" {
		t.Fatalf("SetName invocation did not update value")
	}

	results, err := CallMethod(s, "Sum", 2, 3)
	if err != nil {
		t.Fatalf("CallMethod Sum failed: %v", err)
	}
	if len(results) != 1 || results[0].Int() != 5 {
		t.Fatalf("unexpected Sum results: %v", results)
	}

	valueResults, err := CallMethod(s, "FullName", "Ms. ")
	if err != nil {
		t.Fatalf("CallMethod FullName failed: %v", err)
	}
	if len(valueResults) != 1 || valueResults[0].String() != "Ms. Bob" {
		t.Fatalf("unexpected FullName results: %v", valueResults)
	}
}

func TestFieldCacheConcurrentAccess(t *testing.T) {
	defaultFieldCache.ClearCache()
	tpe := reflect.TypeOf(sampleStruct{})

	const goroutines = 32
	const iterations = 64

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if _, ok := LookupFieldInfo(tpe, "ID"); !ok {
					panic("LookupFieldInfo(ID) failed")
				}
				if _, ok := LookupFieldByTag(tpe, "json", "created_at"); !ok {
					panic("LookupFieldByTag(created_at) failed")
				}
				if _, ok := defaultFieldCache.LookupMethod(reflect.TypeOf(&sampleStruct{}), "SetName"); !ok {
					panic("LookupMethod(SetName) failed")
				}
			}
		}()
	}
	wg.Wait()
}
