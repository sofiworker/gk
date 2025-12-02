//go:build linux

package capture

import (
	"fmt"

	"github.com/sofiworker/gk/gnet/rawcap"
	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

func attachFilterIfAny(handle rawcap.Handle, f Filter) error {
	if len(f.Instructions) == 0 && len(f.Raw) == 0 {
		return nil
	}

	raw, err := toRawInstructions(f)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}

	fd, err := fdFromHandle(handle)
	if err != nil {
		return err
	}

	prog := toSockFprog(raw)
	if err := unix.SetsockoptSockFprog(fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, prog); err != nil {
		return fmt.Errorf("attach bpf: %w", err)
	}
	return nil
}

func fdFromHandle(h rawcap.Handle) (int, error) {
	raw, err := h.RawHandle()
	if err != nil {
		return 0, err
	}
	fd, ok := raw.(int)
	if !ok {
		return 0, fmt.Errorf("unexpected raw handle type %T", raw)
	}
	return fd, nil
}

func toRawInstructions(f Filter) ([]bpf.RawInstruction, error) {
	if len(f.Raw) > 0 {
		return f.Raw, nil
	}
	if len(f.Instructions) == 0 {
		return nil, nil
	}
	return bpf.Assemble(f.Instructions)
}

func toSockFprog(raw []bpf.RawInstruction) *unix.SockFprog {
	flt := make([]unix.SockFilter, len(raw))
	for i, ins := range raw {
		flt[i] = unix.SockFilter{
			Code: ins.Op,
			Jt:   ins.Jt,
			Jf:   ins.Jf,
			K:    ins.K,
		}
	}
	return &unix.SockFprog{
		Len:    uint16(len(flt)),
		Filter: &flt[0],
	}
}
