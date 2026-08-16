package pcapng

import (
	"errors"
	"io"
	"time"

	"golang.org/x/net/bpf"
)

// FilterCopy 读取 pcapng 数据，按 BPF 过滤 EPB 后写入新的 pcapng，返回保留的包数。
// 仅支持单 Section 文件；多 Section 输入请按 section 分段处理（writer
// 不提供多 Section 输出）。
// FilterCopy reads pcapng, filters EPBs by BPF, writes a new pcapng, and
// returns the kept packet count. Single-section files only; split
// multi-section inputs per section (the writer has no multi-section output).
func FilterCopy(r io.Reader, w io.Writer, prog []bpf.Instruction) (int, error) {
	reader := NewReader(r)
	writer, err := NewWriter(w)
	if err != nil {
		return 0, err
	}
	defer writer.Close()

	vm, err := bpf.NewVM(prog)
	if err != nil {
		return 0, err
	}

	idMap := make(map[uint32]uint32)
	count := 0

	for {
		pkt, err := reader.ReadPacket()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return count, nil
			}
			return count, err
		}

		newID, ok := idMap[pkt.InterfaceID]
		if !ok {
			lt, snap, tsRes, okInfo := reader.InterfaceInfo(pkt.InterfaceID)
			if !okInfo {
				lt, snap = 1, 65535
				tsRes = time.Microsecond
			} else if tsRes == 0 {
				tsRes = time.Microsecond
			}
			id, err := writer.AddInterface(lt, snap, WithInterfaceTimestampResolution(tsRes))
			if err != nil {
				return count, err
			}
			idMap[pkt.InterfaceID] = id
			newID = id
		}

		keep, err := vm.Run(pkt.Data)
		if err != nil {
			return count, err
		}
		if keep == 0 {
			continue
		}

		if err := writer.WritePacket(newID, pkt.Data, pkt.Timestamp); err != nil {
			return count, err
		}
		count++
	}
}
