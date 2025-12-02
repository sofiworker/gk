//go:build !linux

package capture

import "github.com/sofiworker/gk/gnet/rawcap"

func attachFilterIfAny(handle rawcap.Handle, f Filter) error {
	// 仅在 Linux 支持 BPF 过滤，其他平台忽略。
	return nil
}
