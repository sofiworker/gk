package pcap

import (
	"io"

	"golang.org/x/net/bpf"
)

// FilterCopy 读取 r 中的 pcap，按 BPF 过滤后写入 w（pcap），返回通过包数。
func FilterCopy(r io.Reader, w io.Writer, prog []bpf.Instruction) (int, error) {
	reader, err := NewReader(r)
	if err != nil {
		return 0, err
	}
	writer, err := NewWriter(w, WithSnapLen(reader.Header().SnapLen), WithLinkType(reader.Header().Network))
	if err != nil {
		return 0, err
	}
	defer writer.Close()

	vm, err := bpf.NewVM(prog)
	if err != nil {
		return 0, err
	}

	count := 0
	for {
		pkt, err := reader.ReadPacket()
		if err != nil {
			if err == io.EOF {
				return count, nil
			}
			return count, err
		}
		keep, err := vm.Run(pkt.Data)
		if err != nil {
			return count, err
		}
		if keep != 0 {
			if err := writer.WritePacket(pkt); err != nil {
				return count, err
			}
			count++
		}
	}
}
