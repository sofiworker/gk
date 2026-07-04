package gerr

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
)

type temporary interface {
	Temporary() bool
}

func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return true
	}
	netErr, ok := AsNetError(err)
	return ok && netErr.Timeout()
}

func IsTemporary(err error) bool {
	if err == nil {
		return false
	}
	var target temporary
	return errors.As(err, &target) && target.Temporary()
}

func IsCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

func IsDeadlineExceeded(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

func IsNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist) || os.IsNotExist(err)
}

func IsPermission(err error) bool {
	return errors.Is(err, os.ErrPermission) || os.IsPermission(err)
}

func IsExist(err error) bool {
	return errors.Is(err, os.ErrExist) || os.IsExist(err)
}

func IsClosed(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, os.ErrClosed) ||
		errors.Is(err, fs.ErrClosed) ||
		errors.Is(err, io.ErrClosedPipe)
}

func AsNetError(err error) (net.Error, bool) {
	var target net.Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func AsNetOpError(err error) (*net.OpError, bool) {
	var target *net.OpError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func AsDNSError(err error) (*net.DNSError, bool) {
	var target *net.DNSError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func AsPathError(err error) (*os.PathError, bool) {
	var target *os.PathError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func AsLinkError(err error) (*os.LinkError, bool) {
	var target *os.LinkError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func AsSyscallError(err error) (*os.SyscallError, bool) {
	var target *os.SyscallError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
