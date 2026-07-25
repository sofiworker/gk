package gerr

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
)

type temporaryErr struct{}

func (temporaryErr) Error() string {
	return "temporary"
}

func (temporaryErr) Temporary() bool {
	return true
}

func TestNetErrorHelpers(t *testing.T) {
	dnsErr := &net.DNSError{
		Err:         "lookup timed out",
		Name:        "example.com",
		IsTimeout:   true,
		IsTemporary: true,
	}
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: dnsErr}
	err := Wrap(opErr, WithMessage("connect"))

	if !IsTimeout(err) {
		t.Fatal("IsTimeout should detect nested net timeout")
	}
	if !IsTemporary(err) {
		t.Fatal("IsTemporary should detect nested temporary net error")
	}
	if _, ok := AsNetError(err); !ok {
		t.Fatal("AsNetError should find net.Error")
	}
	if got, ok := AsNetOpError(err); !ok || got.Op != "dial" {
		t.Fatalf("AsNetOpError = (%v, %v), want dial op", got, ok)
	}
	if got, ok := AsDNSError(err); !ok || got.Name != "example.com" {
		t.Fatalf("AsDNSError = (%v, %v), want example.com", got, ok)
	}
}

func TestContextAndClosedHelpers(t *testing.T) {
	err := Join(context.Canceled, context.DeadlineExceeded)

	if !IsCanceled(err) {
		t.Fatal("IsCanceled should detect context.Canceled")
	}
	if !IsDeadlineExceeded(err) {
		t.Fatal("IsDeadlineExceeded should detect context deadline")
	}
	if !IsTimeout(err) {
		t.Fatal("IsTimeout should detect context deadline")
	}

	if !IsClosed(fmt.Errorf("network close: %w", net.ErrClosed)) {
		t.Fatal("IsClosed should detect net.ErrClosed")
	}
	if !IsClosed(fmt.Errorf("file close: %w", os.ErrClosed)) {
		t.Fatal("IsClosed should detect os.ErrClosed")
	}
	if !IsClosed(fmt.Errorf("pipe close: %w", io.ErrClosedPipe)) {
		t.Fatal("IsClosed should detect io.ErrClosedPipe")
	}
}

func TestOSWrappedErrorHelpers(t *testing.T) {
	pathErr := &os.PathError{Op: "open", Path: "missing.txt", Err: os.ErrNotExist}
	linkErr := &os.LinkError{Op: "link", Old: "old", New: "new", Err: os.ErrPermission}
	syscallErr := &os.SyscallError{Syscall: "open", Err: os.ErrPermission}

	if !IsNotExist(Wrap(pathErr, WithMessage("load config"))) {
		t.Fatal("IsNotExist should detect wrapped path error")
	}
	if !IsPermission(Join(linkErr, syscallErr)) {
		t.Fatal("IsPermission should detect wrapped link/syscall error")
	}
	if got, ok := AsPathError(pathErr); !ok || got.Path != "missing.txt" {
		t.Fatalf("AsPathError = (%v, %v), want missing.txt", got, ok)
	}
	if got, ok := AsLinkError(linkErr); !ok || got.Op != "link" {
		t.Fatalf("AsLinkError = (%v, %v), want link op", got, ok)
	}
	if got, ok := AsSyscallError(syscallErr); !ok || got.Syscall != "open" {
		t.Fatalf("AsSyscallError = (%v, %v), want open syscall", got, ok)
	}
}

func TestTemporaryHelperSupportsTemporaryInterface(t *testing.T) {
	if !IsTemporary(Wrap(temporaryErr{}, WithMessage("retry later"))) {
		t.Fatal("IsTemporary should detect Temporary method")
	}
}
