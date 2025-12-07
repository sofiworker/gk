package gserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sofiworker/gk/gcodec"
)

func TestCodecFactoryExtra(t *testing.T) {
	cf := newCodecFactory()
	
	// Test Default
	if cf.Get("application/json") == nil {
		t.Error("json should be registered")
	}
	
	// Test Register
	mock := gcodec.NewPlainCodec()
	err := cf.Register("application/mock", mock)
	if err != nil {
		t.Errorf("Register failed: %v", err)
	}
	
	if cf.Get("application/mock") != mock {
		t.Error("Get failed")
	}
	
	// Test Normalize
	if cf.Get("APPLICATION/JSON; charset=utf-8") == nil {
		t.Error("normalize failed")
	}
	
	// Test Duplicate
	if err := cf.Register("application/json", mock); err != ErrAlreadyRegistered {
		t.Errorf("expected duplicate error, got %v", err)
	}
	
	// Test Nil
	if err := cf.Register("bad", nil); err != ErrInvalidCodec {
		t.Errorf("expected invalid error, got %v", err)
	}
}

func TestConfigOptions(t *testing.T) {
	c := &Config{}
	
	WithMatcher(nil)(c) // Should not set nil
	// We need a mock matcher to test positive case
	
	cf := newCodecFactory()
	WithCodec(cf)(c)
	if c.codec != cf { t.Error("WithCodec failed") }
	
	WithLogger(nil)(c) // Should not set nil
	
	WithUseRawPath(true)(c)
	if !c.UseRawPath { t.Error("WithUseRawPath failed") }
}

func TestUtilFunctions(t *testing.T) {
	// JoinPaths
	if p := JoinPaths("/a", "b"); p != "/a/b" { t.Errorf("JoinPaths failed: %s", p) }
	if p := JoinPaths("/a", ""); p != "/a" { t.Errorf("JoinPaths failed: %s", p) }
	if p := JoinPaths("/a", "b/"); p != "/a/b/" { t.Errorf("JoinPaths failed: %s", p) }
	
	// CheckPathValid
	defer func() {
		if r := recover(); r == nil {
			t.Error("CheckPathValid('') did not panic")
		}
	}()
	CheckPathValid("")
	
	// Other panic cases can be tested similarly...
	// CheckPathValid("no_slash") -> panic
}

func TestParseHTTPVersion(t *testing.T) {
	maj, min, ok := ParseHTTPVersion("HTTP/1.1")
	if !ok || maj != 1 || min != 1 { t.Error("HTTP/1.1 failed") }
	
	maj, min, ok = ParseHTTPVersion("HTTP/2")
	if !ok || maj != 2 || min != 0 { t.Error("HTTP/2 failed") }
	
	_, _, ok = ParseHTTPVersion("BAD")
	if ok { t.Error("BAD should fail") }
}

func TestStdResponseWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	w := wrapResponseWriter(rec)
	
	if w.Status() != http.StatusOK { t.Error("initial status not 200") }
	if w.Written() { t.Error("initial written true") }
	
	w.WriteHeader(http.StatusNotFound)
	if w.Status() != http.StatusNotFound { t.Error("status not 404") }
	if !w.Written() { t.Error("written false after WriteHeader") }
	
	n, err := w.WriteString("hello")
	if err != nil { t.Fatal(err) }
	if n != 5 { t.Errorf("n=%d", n) }
	if w.Size() != 5 { t.Errorf("size=%d", w.Size()) }
}

func TestLoggerWrapper(t *testing.T) {
	l := newLogger()
	// Just call methods to ensure no panic
	l.Debugf("test")
	l.Infof("test")
	l.Warnf("test")
	l.Errorf("test")
}

func TestMethodMatcher_Extra(t *testing.T) {
	// Test extractPathFeature
	mr := newMethodMatcher()
	f := mr.extractPathFeature("/a/:b/*c")
	if !f.hasParam { t.Error("param missing") }
	if !f.hasWild { t.Error("wild missing") }
	if f.segmentCnt != 3 { t.Errorf("seg count %d", f.segmentCnt) }
}
