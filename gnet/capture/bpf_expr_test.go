package capture

import (
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/bpf"
)

// tcpdumpAvailable 检测 tcpdump 是否存在（对照测试依赖它）。
// tcpdumpAvailable reports whether tcpdump exists (comparison tests need it).
func tcpdumpAvailable() bool {
	_, err := exec.LookPath("tcpdump")
	return err == nil
}

// parseTcpdumpDD 解析 `tcpdump -dd` 输出为指令序列。
// parseTcpdumpDD parses `tcpdump -dd` output into instructions.
func parseTcpdumpDD(t *testing.T, out string) []bpf.Instruction {
	t.Helper()
	var insns []bpf.Instruction
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimSuffix(strings.TrimPrefix(line, "{"), "},")
		fields := strings.Split(line, ",")
		var parts []uint32
		for _, f := range fields {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			v, err := strconv.ParseUint(f, 0, 32)
			if err != nil {
				t.Fatalf("parse %q: %v", f, err)
			}
			parts = append(parts, uint32(v))
		}
		if len(parts) != 4 {
			t.Fatalf("bad line %q", line)
		}
		code, jt, jf, k := parts[0], uint8(parts[1]), uint8(parts[2]), parts[3]
		switch {
		case code == 0x28 || code == 0x20: // LD W ABS
			insns = append(insns, bpf.LoadAbsolute{Size: 4, Off: k})
		case code == 0x30: // LD B ABS
			insns = append(insns, bpf.LoadAbsolute{Size: 1, Off: k})
		case code == 0x48: // LD H IND
			insns = append(insns, bpf.LoadIndirect{Size: 2, Off: k})
		case code == 0xb1: // LDX B MSH
			insns = append(insns, bpf.LoadMemShift{Off: k})
		case code == 0x54: // ALU AND K
			insns = append(insns, bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: k})
		case code == 0x15:
			insns = append(insns, bpf.JumpIf{Cond: bpf.JumpEqual, Val: k, SkipTrue: jt, SkipFalse: jf})
		case code == 0x35:
			insns = append(insns, bpf.JumpIf{Cond: bpf.JumpGreaterOrEqual, Val: k, SkipTrue: jt, SkipFalse: jf})
		case code == 0x25:
			insns = append(insns, bpf.JumpIf{Cond: bpf.JumpGreaterThan, Val: k, SkipTrue: jt, SkipFalse: jf})
		case code == 0x45:
			insns = append(insns, bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: k, SkipTrue: jt, SkipFalse: jf})
		case code == 0x06:
			insns = append(insns, bpf.RetConstant{Val: k})
		default:
			t.Fatalf("unsupported opcode %#x", code)
		}
	}
	return insns
}

// TestCompileExprGolden 与 tcpdump -dd 逐字节对照基础原语。
// TestCompileExprGolden byte-compares base primitives with tcpdump -dd.
func TestCompileExprGolden(t *testing.T) {
	if !tcpdumpAvailable() {
		t.Skip("tcpdump not available")
	}
	exprs := []string{
		"arp",
		"ip",
		"ip6",
		"tcp",
		"udp",
		"icmp",
		"host 192.168.1.1",
		"src host 192.168.1.1",
		"dst host 192.168.1.1",
		"net 10.0.0.0/24",
		"port 80",
		"src port 12345",
		"dst port 53",
		"portrange 1-100",
	}
	for _, expr := range exprs {
		t.Run(expr, func(t *testing.T) {
			wantOut, err := exec.Command("tcpdump", "-dd", expr).Output()
			if err != nil {
				t.Fatalf("tcpdump: %v", err)
			}
			want := parseTcpdumpDD(t, string(wantOut))
			got, err := CompileExpr(expr, LinkTypeEN10MB)
			if err != nil {
				t.Fatalf("CompileExpr: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("instructions mismatch:\ngot:  %v\nwant: %v", got, want)
			}
		})
	}
}

// TestCompileExprErrors 覆盖错误输入。
// TestCompileExprErrors covers bad input.
func TestCompileExprErrors(t *testing.T) {
	for _, expr := range []string{
		"",
		"tcp and",
		"host notanip",
		"port",
		"portrange 1",
		"unknownkeyword",
		"net 10.0.0.0/33",
		"(tcp",
	} {
		if _, err := CompileExpr(expr, LinkTypeEN10MB); err == nil {
			t.Errorf("CompileExpr(%q) = nil error", expr)
		}
	}
}
