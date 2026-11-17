package capture

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"golang.org/x/net/bpf"
)

// 说明：本文件实现 tcpdump 表达式子集的纯 Go 编译器。
// 基础原语（arp/ip/ip6/tcp/udp/icmp/host/net/port/portrange 及 src/dst 限定）
// 按 libpcap gencode 的指令模板逐字节生成（与 tcpdump -dd 一致，见测试）；
// 组合子（and/or/not/括号）用 accept/reject 修复点组装，语义与 libpcap 等价。
// 仅支持 EN10MB（以太网）链路类型；不支持主机名与 IPv6 字面量。
//
// This file implements a pure-Go compiler for a subset of tcpdump expressions.
// Base primitives (arp/ip/ip6/tcp/udp/icmp/host/net/port/portrange with
// src/dst qualifiers) are emitted byte-for-byte per libpcap's gencode
// templates (matching tcpdump -dd; see tests); combinators (and/or/not and
// parentheses) assemble via accept/reject fixups with libpcap-equivalent
// semantics. Only EN10MB (Ethernet) link types are supported; hostnames and
// IPv6 literals are not resolved.

// LinkTypeEN10MB 是以太网链路类型号。
// LinkTypeEN10MB is the Ethernet link type number.
const LinkTypeEN10MB = 1

// CompileExpr 编译 tcpdump 表达式子集为 BPF 指令。
// CompileExpr compiles the tcpdump expression subset into BPF instructions.
func CompileExpr(expr string, linkType int) ([]bpf.Instruction, error) {
	if linkType != 0 && linkType != LinkTypeEN10MB {
		return nil, fmt.Errorf("capture: unsupported link type %d (only EN10MB)", linkType)
	}
	toks, err := tokenize(expr)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != "eof" {
		return nil, fmt.Errorf("capture: unexpected token %q", p.peek().text)
	}
	return compile(node), nil
}

// ---- 词法 ----

type token struct {
	kind string // ident / ip / num / lparen / rparen / dash / slash / eof
	text string
	ip   net.IP
	num  uint32
}

func tokenize(s string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case c == '(':
			toks = append(toks, token{kind: "lparen", text: "("})
			i++
		case c == ')':
			toks = append(toks, token{kind: "rparen", text: ")"})
			i++
		case c == '-':
			toks = append(toks, token{kind: "dash", text: "-"})
			i++
		case c == '/':
			toks = append(toks, token{kind: "slash", text: "/"})
			i++
		case c >= '0' && c <= '9':
			j := i
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			text := s[i:j]
			if strings.Count(text, ".") == 3 {
				ip := net.ParseIP(text)
				if ip == nil || ip.To4() == nil {
					return nil, fmt.Errorf("capture: invalid IP %q", text)
				}
				toks = append(toks, token{kind: "ip", text: text, ip: ip.To4()})
			} else {
				n, err := strconv.ParseUint(text, 10, 32)
				if err != nil {
					return nil, fmt.Errorf("capture: invalid number %q", text)
				}
				toks = append(toks, token{kind: "num", text: text, num: uint32(n)})
			}
			i = j
		default:
			j := i
			for j < len(s) && isIdentChar(s[j]) {
				j++
			}
			toks = append(toks, token{kind: "ident", text: s[i:j]})
			i = j
		}
	}
	toks = append(toks, token{kind: "eof"})
	return toks, nil
}

func isIdentChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= '0' && c <= '9'
}

// ---- 解析 ----

type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) next() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) expect(kind string) (token, error) {
	t := p.peek()
	if t.kind != kind {
		return t, fmt.Errorf("capture: expected %s, got %q", kind, t.text)
	}
	return p.next(), nil
}

func (p *parser) isIdent(s string) bool {
	t := p.peek()
	return t.kind == "ident" && strings.EqualFold(t.text, s)
}

func (p *parser) takeIdent(s string) bool {
	if p.isIdent(s) {
		p.next()
		return true
	}
	return false
}

// predicate 是编译单元：emit 时把自己的 accept/reject 修复点注册进 builder。
// predicate is a compilation unit: emit registers its accept/reject fixups
// with the builder.
type predicate interface{ emit(b *builder) }

// tmplPred 是逐字节模板原语。
// tmplPred is a byte-exact template primitive.
type tmplPred struct {
	insns          []bpf.Instruction
	accRet, rejRet int
}

func (p tmplPred) emit(b *builder) { b.emitTemplate(p.insns, p.accRet, p.rejRet) }

// newTmpl 从模板函数构造原语。
// newTmpl builds a primitive from a template function.
func newTmpl(f func() ([]bpf.Instruction, int, int)) tmplPred {
	i, a, r := f()
	return tmplPred{insns: i, accRet: a, rejRet: r}
}

type hostPred struct {
	src, dst bool
	ip       net.IP
}

func (p hostPred) emit(b *builder) {
	if p.src && !p.dst {
		b.emitTemplate(srcHostTemplate(ipU32(p.ip)))
		return
	}
	if p.dst && !p.src {
		b.emitTemplate(dstHostTemplate(ipU32(p.ip)))
		return
	}
	b.emitTemplate(hostTemplate(ipU32(p.ip)))
}

type netPred struct {
	ip   net.IP
	mask net.IPMask
}

func (p netPred) emit(b *builder) {
	b.emitTemplate(netTemplate(ipU32(p.ip), maskU32(p.mask)))
}

type portPred struct {
	src, dst bool
	rng      bool
	lo, hi   uint32
}

func (p portPred) emit(b *builder) {
	switch {
	case p.rng:
		b.emitTemplate(portrangeTemplate(p.lo, p.hi))
	case p.src && !p.dst:
		b.emitTemplate(srcPortTemplate(p.lo))
	case p.dst && !p.src:
		b.emitTemplate(dstPortTemplate(p.lo))
	default:
		b.emitTemplate(portTemplate(p.lo))
	}
}

type notPred struct{ inner predicate }
type andPred struct{ left, right predicate }
type orPred struct{ left, right predicate }

func (p notPred) emit(b *builder) { b.emitNot(p.inner) }
func (p andPred) emit(b *builder) { b.emitAnd(p.left, p.right) }
func (p orPred) emit(b *builder)  { b.emitOr(p.left, p.right) }

func (p *parser) parseOr() (predicate, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isIdent("or") {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orPred{left, right}
	}
	return left, nil
}

func (p *parser) parseAnd() (predicate, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	// 显式 "and" 或相邻原语的隐式 and（如 "udp port 53"）。
	for p.isIdent("and") || p.startsAtom() {
		if p.isIdent("and") {
			p.next()
		}
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = andPred{left, right}
	}
	return left, nil
}

// startsAtom 判断下一个 token 是否开启一个新原语。
// startsAtom reports whether the next token starts a new atom.
func (p *parser) startsAtom() bool {
	t := p.peek()
	if t.kind == "lparen" {
		return true
	}
	if t.kind != "ident" {
		return false
	}
	switch strings.ToLower(t.text) {
	case "tcp", "udp", "icmp", "arp", "ip", "ip6", "host", "net", "port", "portrange", "src", "dst", "not":
		return true
	}
	return false
}

func (p *parser) parseNot() (predicate, error) {
	if p.takeIdent("not") {
		inner, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return notPred{inner}, nil
	}
	return p.parseAtom()
}

func (p *parser) parseAtom() (predicate, error) {
	if p.peek().kind == "lparen" {
		p.next()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect("rparen"); err != nil {
			return nil, err
		}
		return inner, nil
	}

	var src, dst bool
	switch {
	case p.takeIdent("src"):
		src = true
	case p.takeIdent("dst"):
		dst = true
	}

	t := p.peek()
	if t.kind != "ident" {
		return nil, fmt.Errorf("capture: unexpected token %q", t.text)
	}
	switch kw := strings.ToLower(t.text); kw {
	case "tcp":
		p.next()
		return newTmpl(tcpTemplate), nil
	case "udp":
		p.next()
		return newTmpl(udpTemplate), nil
	case "icmp":
		p.next()
		return newTmpl(icmpTemplate), nil
	case "arp":
		p.next()
		return newTmpl(func() ([]bpf.Instruction, int, int) { return arpTemplate(0x0806) }), nil
	case "ip":
		p.next()
		return newTmpl(func() ([]bpf.Instruction, int, int) { return arpTemplate(0x0800) }), nil
	case "ip6":
		p.next()
		return newTmpl(func() ([]bpf.Instruction, int, int) { return arpTemplate(0x86dd) }), nil
	case "host":
		p.next()
		ip, err := p.parseIP()
		if err != nil {
			return nil, err
		}
		return hostPred{src: src, dst: dst, ip: ip}, nil
	case "net":
		p.next()
		return p.parseNet()
	case "port", "portrange":
		p.next()
		rng := kw == "portrange"
		n, err := p.expect("num")
		if err != nil {
			return nil, err
		}
		lo, hi := n.num, n.num
		if rng {
			if _, err := p.expect("dash"); err != nil {
				return nil, err
			}
			n2, err := p.expect("num")
			if err != nil {
				return nil, err
			}
			hi = n2.num
		}
		return portPred{src: src, dst: dst, rng: rng, lo: lo, hi: hi}, nil
	}
	return nil, fmt.Errorf("capture: unknown keyword %q", t.text)
}

func (p *parser) parseIP() (net.IP, error) {
	t, err := p.expect("ip")
	if err != nil {
		return nil, err
	}
	return t.ip, nil
}

func (p *parser) parseNet() (predicate, error) {
	ipTok, err := p.expect("ip")
	if err != nil {
		return nil, err
	}
	ip := ipTok.ip
	mask := net.CIDRMask(32, 32)
	if p.takeIdent("mask") {
		mt, err := p.expect("ip")
		if err != nil {
			return nil, err
		}
		mask = net.IPMask(mt.ip.To4())
	} else if p.peek().kind == "slash" {
		p.next()
		n, err := p.expect("num")
		if err != nil {
			return nil, err
		}
		if n.num > 32 {
			return nil, fmt.Errorf("capture: invalid prefix length %d", n.num)
		}
		mask = net.CIDRMask(int(n.num), 32)
	}
	return netPred{ip: ip.Mask(mask), mask: mask}, nil
}

// ---- 代码生成 ----

// fixup 是一次待回填的跳转（经 builder 切片索引回写值类型指令）。
// fixup is a pending jump patch (written back through the builder's slice as
// a value type, since the x/net VM only matches value types).
type fixup func(b *builder, target int)

// builder 收集指令与 accept/reject 修复点（libpcap 块模型）。
// builder collects instructions and accept/reject fixups (libpcap block
// model).
type builder struct {
	insns []bpf.Instruction
	accF  []fixup
	rejF  []fixup
}

// emitTemplate 复制模板；模板内两条 ret 位置接到当前 accept/reject 分支。
// emitTemplate copies a template; its two ret slots attach to the current
// accept/reject branches.
func (b *builder) emitTemplate(insns []bpf.Instruction, accRet, rejRet int) {
	base := len(b.insns)
	b.insns = append(b.insns, insns...)
	// 模板尾部 ret 改为“恒真条件跳转”（jeq #0 双分支同目标）：
	// x/net 的 VM 不支持无条件 ja，此形态等价且内核/VM 双兼容。
	// The template's two rets become always-true conditional jumps
	// (jeq #0 with both branches to the same target): the x/net VM lacks
	// unconditional ja; this form is equivalent and works on both.
	b.insns[base+accRet] = bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0}
	accIdx := base + accRet
	b.accF = append(b.accF, func(bb *builder, t int) {
		j := bb.insns[accIdx].(bpf.JumpIf)
		j.SkipTrue = uint8(t - accIdx - 1)
		j.SkipFalse = j.SkipTrue
		bb.insns[accIdx] = j
	})
	b.insns[base+rejRet] = bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0}
	rejIdx := base + rejRet
	b.rejF = append(b.rejF, func(bb *builder, t int) {
		j := bb.insns[rejIdx].(bpf.JumpIf)
		j.SkipTrue = uint8(t - rejIdx - 1)
		j.SkipFalse = j.SkipTrue
		bb.insns[rejIdx] = j
	})
}

// emitAnd 组合 left AND right：left 真 → 继续 right；left 假 → 全局 reject。
// emitAnd composes left AND right: left-true continues into right, left-false
// goes to the global reject.
func (b *builder) emitAnd(left, right predicate) {
	markA, markR := len(b.accF), len(b.rejF)
	left.emit(b)
	leftAcc := b.accF[markA:]
	leftRej := b.rejF[markR:]
	b.accF = b.accF[:markA]
	b.rejF = b.rejF[:markR]
	rightStart := len(b.insns)
	for _, f := range leftAcc {
		f(b, rightStart)
	}
	b.rejF = append(b.rejF, leftRej...)
	right.emit(b)
}

// emitOr 组合 left OR right：left 真 → 全局 accept；left 假 → 继续 right。
// emitOr composes left OR right: left-true goes to the global accept,
// left-false continues into right.
func (b *builder) emitOr(left, right predicate) {
	markA, markR := len(b.accF), len(b.rejF)
	left.emit(b)
	leftAcc := b.accF[markA:]
	leftRej := b.rejF[markR:]
	b.accF = b.accF[:markA]
	b.rejF = b.rejF[:markR]
	rightStart := len(b.insns)
	for _, f := range leftRej {
		f(b, rightStart)
	}
	b.accF = append(b.accF, leftAcc...)
	right.emit(b)
}

// emitNot 取反：inner 的 accept → 全局 reject，reject → 全局 accept。
// emitNot negates: inner accept → global reject, inner reject → global
// accept.
func (b *builder) emitNot(inner predicate) {
	markA, markR := len(b.accF), len(b.rejF)
	inner.emit(b)
	innerAcc := b.accF[markA:]
	innerRej := b.rejF[markR:]
	b.accF = b.accF[:markA]
	b.rejF = b.rejF[:markR]
	b.rejF = append(b.rejF, innerAcc...)
	b.accF = append(b.accF, innerRej...)
}

// templateOf 返回谓词对应的单模板（组合子返回 nil）。
// templateOf returns the predicate's single template (nil for combinators).
func templateOf(p predicate) []bpf.Instruction {
	switch t := p.(type) {
	case tmplPred:
		return t.insns
	case hostPred:
		if t.src && !t.dst {
			i, _, _ := srcHostTemplate(ipU32(t.ip))
			return i
		}
		if t.dst && !t.src {
			i, _, _ := dstHostTemplate(ipU32(t.ip))
			return i
		}
		i, _, _ := hostTemplate(ipU32(t.ip))
		return i
	case netPred:
		i, _, _ := netTemplate(ipU32(t.ip), maskU32(t.mask))
		return i
	case portPred:
		switch {
		case t.rng:
			i, _, _ := portrangeTemplate(t.lo, t.hi)
			return i
		case t.src && !t.dst:
			i, _, _ := srcPortTemplate(t.lo)
			return i
		case t.dst && !t.src:
			i, _, _ := dstPortTemplate(t.lo)
			return i
		default:
			i, _, _ := portTemplate(t.lo)
			return i
		}
	}
	return nil
}

// compile 组装全部修复点并产出最终指令。顶层单模板直接逐字节返回，
// 与 tcpdump -dd 完全一致。
//
// compile resolves all fixups and produces the final program. A top-level
// single template is returned byte-exactly, matching tcpdump -dd.
func compile(root predicate) []bpf.Instruction {
	if ins := templateOf(root); ins != nil {
		return ins
	}
	b := &builder{}
	root.emit(b)
	accIdx := len(b.insns)
	b.insns = append(b.insns, bpf.RetConstant{Val: 0x40000})
	rejIdx := len(b.insns)
	b.insns = append(b.insns, bpf.RetConstant{Val: 0})
	for _, f := range b.accF {
		f(b, accIdx)
	}
	for _, f := range b.rejF {
		f(b, rejIdx)
	}
	return b.insns
}

// ---- libpcap 指令模板（与 tcpdump -dd 逐字节一致） ----

func ipU32(ip net.IP) uint32 {
	ip4 := ip.To4()
	return uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
}

func maskU32(m net.IPMask) uint32 { return ipU32(net.IP(m)) }

// arpTemplate 生成 ethertype 单值检查（arp/ip/ip6 共用）。
func arpTemplate(etherType uint32) ([]bpf.Instruction, int, int) {
	return []bpf.Instruction{
		bpf.LoadAbsolute{Size: 4, Off: 12},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: etherType, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 0x40000},
		bpf.RetConstant{Val: 0},
	}, 2, 3
}

func icmpTemplate() ([]bpf.Instruction, int, int) {
	return []bpf.Instruction{
		bpf.LoadAbsolute{Size: 4, Off: 12},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 0, SkipFalse: 3},
		bpf.LoadAbsolute{Size: 1, Off: 23},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 1, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 0x40000},
		bpf.RetConstant{Val: 0},
	}, 4, 5
}

func tcpTemplate() ([]bpf.Instruction, int, int) { return protoTemplate(6) }
func udpTemplate() ([]bpf.Instruction, int, int) { return protoTemplate(17) }

func protoTemplate(proto uint32) ([]bpf.Instruction, int, int) {
	return []bpf.Instruction{
		bpf.LoadAbsolute{Size: 4, Off: 12},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 0, SkipFalse: 2},
		bpf.LoadAbsolute{Size: 1, Off: 23},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: proto, SkipTrue: 6, SkipFalse: 7},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x86dd, SkipTrue: 0, SkipFalse: 6},
		bpf.LoadAbsolute{Size: 1, Off: 20},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: proto, SkipTrue: 3, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x2c, SkipTrue: 0, SkipFalse: 3},
		bpf.LoadAbsolute{Size: 1, Off: 54},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: proto, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 0x40000},
		bpf.RetConstant{Val: 0},
	}, 10, 11
}

func hostTemplate(ip uint32) ([]bpf.Instruction, int, int) {
	return []bpf.Instruction{
		bpf.LoadAbsolute{Size: 4, Off: 12},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 0, SkipFalse: 4},
		bpf.LoadAbsolute{Size: 4, Off: 26},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: ip, SkipTrue: 8, SkipFalse: 0},
		bpf.LoadAbsolute{Size: 4, Off: 30},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: ip, SkipTrue: 6, SkipFalse: 7},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0806, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x8035, SkipTrue: 0, SkipFalse: 5},
		bpf.LoadAbsolute{Size: 4, Off: 28},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: ip, SkipTrue: 2, SkipFalse: 0},
		bpf.LoadAbsolute{Size: 4, Off: 38},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: ip, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 0x40000},
		bpf.RetConstant{Val: 0},
	}, 12, 13
}

func srcHostTemplate(ip uint32) ([]bpf.Instruction, int, int) {
	return []bpf.Instruction{
		bpf.LoadAbsolute{Size: 4, Off: 12},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 0, SkipFalse: 2},
		bpf.LoadAbsolute{Size: 4, Off: 26},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: ip, SkipTrue: 4, SkipFalse: 5},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0806, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x8035, SkipTrue: 0, SkipFalse: 3},
		bpf.LoadAbsolute{Size: 4, Off: 28},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: ip, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 0x40000},
		bpf.RetConstant{Val: 0},
	}, 8, 9
}

func dstHostTemplate(ip uint32) ([]bpf.Instruction, int, int) {
	return []bpf.Instruction{
		bpf.LoadAbsolute{Size: 4, Off: 12},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 0, SkipFalse: 2},
		bpf.LoadAbsolute{Size: 4, Off: 30},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: ip, SkipTrue: 4, SkipFalse: 5},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0806, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x8035, SkipTrue: 0, SkipFalse: 3},
		bpf.LoadAbsolute{Size: 4, Off: 38},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: ip, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 0x40000},
		bpf.RetConstant{Val: 0},
	}, 8, 9
}

func netTemplate(netv, mask uint32) ([]bpf.Instruction, int, int) {
	return []bpf.Instruction{
		bpf.LoadAbsolute{Size: 4, Off: 12},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 0, SkipFalse: 6},
		bpf.LoadAbsolute{Size: 4, Off: 26},
		bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: mask},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: netv, SkipTrue: 11, SkipFalse: 0},
		bpf.LoadAbsolute{Size: 4, Off: 30},
		bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: mask},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: netv, SkipTrue: 8, SkipFalse: 9},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0806, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x8035, SkipTrue: 0, SkipFalse: 7},
		bpf.LoadAbsolute{Size: 4, Off: 28},
		bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: mask},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: netv, SkipTrue: 3, SkipFalse: 0},
		bpf.LoadAbsolute{Size: 4, Off: 38},
		bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: mask},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: netv, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 0x40000},
		bpf.RetConstant{Val: 0},
	}, 16, 17
}

func portTemplate(port uint32) ([]bpf.Instruction, int, int) {
	return []bpf.Instruction{
		bpf.LoadAbsolute{Size: 4, Off: 12},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x86dd, SkipTrue: 0, SkipFalse: 8},
		bpf.LoadAbsolute{Size: 1, Off: 20},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x84, SkipTrue: 2, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipTrue: 0, SkipFalse: 17},
		bpf.LoadAbsolute{Size: 4, Off: 54},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: port, SkipTrue: 14, SkipFalse: 0},
		bpf.LoadAbsolute{Size: 4, Off: 56},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: port, SkipTrue: 12, SkipFalse: 13},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 0, SkipFalse: 12},
		bpf.LoadAbsolute{Size: 1, Off: 23},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x84, SkipTrue: 2, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipTrue: 0, SkipFalse: 8},
		bpf.LoadAbsolute{Size: 4, Off: 20},
		bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: 0x1fff, SkipTrue: 6, SkipFalse: 0},
		bpf.LoadMemShift{Off: 14},
		bpf.LoadIndirect{Size: 2, Off: 14},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: port, SkipTrue: 2, SkipFalse: 0},
		bpf.LoadIndirect{Size: 2, Off: 16},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: port, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 0x40000},
		bpf.RetConstant{Val: 0},
	}, 22, 23
}

func srcPortTemplate(port uint32) ([]bpf.Instruction, int, int) {
	return []bpf.Instruction{
		bpf.LoadAbsolute{Size: 4, Off: 12},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x86dd, SkipTrue: 0, SkipFalse: 6},
		bpf.LoadAbsolute{Size: 1, Off: 20},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x84, SkipTrue: 2, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipTrue: 0, SkipFalse: 13},
		bpf.LoadAbsolute{Size: 4, Off: 54},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: port, SkipTrue: 10, SkipFalse: 11},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 0, SkipFalse: 10},
		bpf.LoadAbsolute{Size: 1, Off: 23},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x84, SkipTrue: 2, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipTrue: 0, SkipFalse: 6},
		bpf.LoadAbsolute{Size: 4, Off: 20},
		bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: 0x1fff, SkipTrue: 4, SkipFalse: 0},
		bpf.LoadMemShift{Off: 14},
		bpf.LoadIndirect{Size: 2, Off: 14},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: port, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 0x40000},
		bpf.RetConstant{Val: 0},
	}, 18, 19
}

func dstPortTemplate(port uint32) ([]bpf.Instruction, int, int) {
	return []bpf.Instruction{
		bpf.LoadAbsolute{Size: 4, Off: 12},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x86dd, SkipTrue: 0, SkipFalse: 6},
		bpf.LoadAbsolute{Size: 1, Off: 20},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x84, SkipTrue: 2, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipTrue: 0, SkipFalse: 13},
		bpf.LoadAbsolute{Size: 4, Off: 56},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: port, SkipTrue: 10, SkipFalse: 11},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 0, SkipFalse: 10},
		bpf.LoadAbsolute{Size: 1, Off: 23},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x84, SkipTrue: 2, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipTrue: 0, SkipFalse: 6},
		bpf.LoadAbsolute{Size: 4, Off: 20},
		bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: 0x1fff, SkipTrue: 4, SkipFalse: 0},
		bpf.LoadMemShift{Off: 14},
		bpf.LoadIndirect{Size: 2, Off: 16},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: port, SkipTrue: 0, SkipFalse: 1},
		bpf.RetConstant{Val: 0x40000},
		bpf.RetConstant{Val: 0},
	}, 18, 19
}

func portrangeTemplate(lo, hi uint32) ([]bpf.Instruction, int, int) {
	return []bpf.Instruction{
		bpf.LoadAbsolute{Size: 4, Off: 12},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x86dd, SkipTrue: 0, SkipFalse: 9},
		bpf.LoadAbsolute{Size: 1, Off: 20},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x84, SkipTrue: 2, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipTrue: 0, SkipFalse: 20},
		bpf.LoadAbsolute{Size: 4, Off: 54},
		bpf.JumpIf{Cond: bpf.JumpGreaterOrEqual, Val: lo, SkipTrue: 0, SkipFalse: 1},
		bpf.JumpIf{Cond: bpf.JumpGreaterThan, Val: hi, SkipTrue: 0, SkipFalse: 16},
		bpf.LoadAbsolute{Size: 4, Off: 56},
		bpf.JumpIf{Cond: bpf.JumpGreaterOrEqual, Val: lo, SkipTrue: 13, SkipFalse: 15},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipTrue: 0, SkipFalse: 14},
		bpf.LoadAbsolute{Size: 1, Off: 23},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x84, SkipTrue: 2, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipTrue: 1, SkipFalse: 0},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 17, SkipTrue: 0, SkipFalse: 10},
		bpf.LoadAbsolute{Size: 4, Off: 20},
		bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: 0x1fff, SkipTrue: 8, SkipFalse: 0},
		bpf.LoadMemShift{Off: 14},
		bpf.LoadIndirect{Size: 2, Off: 14},
		bpf.JumpIf{Cond: bpf.JumpGreaterOrEqual, Val: lo, SkipTrue: 0, SkipFalse: 1},
		bpf.JumpIf{Cond: bpf.JumpGreaterThan, Val: hi, SkipTrue: 0, SkipFalse: 3},
		bpf.LoadIndirect{Size: 2, Off: 16},
		bpf.JumpIf{Cond: bpf.JumpGreaterOrEqual, Val: lo, SkipTrue: 0, SkipFalse: 2},
		bpf.JumpIf{Cond: bpf.JumpGreaterThan, Val: hi, SkipTrue: 1, SkipFalse: 0},
		bpf.RetConstant{Val: 0x40000},
		bpf.RetConstant{Val: 0},
	}, 25, 26
}
