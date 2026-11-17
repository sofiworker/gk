package ghttp

import (
	"math/rand"
	"testing"
)

// validRequestPathRef 是优化前 validRequestPath 的原始实现，作为等价性验证的参照。
// validRequestPathRef is the pre-optimization implementation, kept as the reference
// for equivalence verification.
func validRequestPathRef(p string, strict bool) bool {
	if p == "" || p[0] != '/' {
		return false
	}
	start := 1
	for i := 1; i <= len(p); i++ {
		if i < len(p) {
			c := p[i]
			if c < 0x20 || c == 0x7f {
				return false
			}
			if c != '/' {
				continue
			}
		}
		seg := p[start:i]
		switch {
		case seg == "." || seg == "..":
			return false
		case seg == "" && strict && i < len(p):
			return false
		}
		start = i + 1
	}
	return true
}

// TestValidRequestPathEquivalence 对参照实现穷举验证优化后的行为一致性：
// 字母表 {'/', '.', 'a', 控制字符} 覆盖全部长度 ≤ 10 的路径（两种 strict 取值），
// 再用更丰富的字母表随机生成长路径。
// TestValidRequestPathEquivalence exhaustively verifies the optimized implementation
// against the reference over all paths of length ≤ 10 from {'/', '.', 'a', control},
// for both strict values, plus randomized longer paths from a richer alphabet.
func TestValidRequestPathEquivalence(t *testing.T) {
	check := func(p string, strict bool) {
		want := validRequestPathRef(p, strict)
		if got := validRequestPath(p, strict); got != want {
			t.Fatalf("validRequestPath(%q, strict=%v) = %v, want %v", p, strict, got, want)
		}
	}

	// 穷举：4 字母表、长度 ≤ 10、含空串；首个字符任意（合法与否都要一致）。
	// Exhaustive: 4-letter alphabet, all lengths ≤ 10 including the empty string.
	alphabet := []byte{'/', '.', 'a', 0x01}
	var buf [10]byte
	var walk func(depth int)
	walk = func(depth int) {
		p := string(buf[:depth])
		check(p, true)
		check(p, false)
		if depth == len(buf) {
			return
		}
		for _, c := range alphabet {
			buf[depth] = c
			walk(depth + 1)
		}
	}
	walk(0)

	// 随机长路径：更丰富的字母表，含 0x7f、'-'、多个点与斜杠。
	// Randomized long paths from a richer alphabet including 0x7f, '-', dots.
	rng := rand.New(rand.NewSource(42))
	rich := []byte{'/', '.', 'a', 'b', '-', 0x01, 0x7f}
	for i := 0; i < 200000; i++ {
		n := rng.Intn(64)
		b := make([]byte, n)
		for j := range b {
			b[j] = rich[rng.Intn(len(rich))]
		}
		p := string(b)
		strict := rng.Intn(2) == 0
		check(p, strict)
	}

	// 手工边界样例，确保可读的回归锚点。
	// Hand-picked boundary cases as readable regression anchors.
	samples := []struct {
		p      string
		strict bool
		want   bool
	}{
		{"/", true, true},
		{"/a", true, true},
		{"/a/", true, true},    // 尾部单斜杠允许 / trailing slash allowed
		{"/a//b", true, false}, // 内部空段 / interior empty segment
		{"/a//b", false, true},
		{"/a//", true, false},
		{"/.", true, false},
		{"/..", true, false},
		{"/./", true, false},
		{"/../a", true, false},
		{"/a/./b", true, false},
		{"/a/../b", true, false},
		{"/a/.", true, false},
		{"/a/..", true, false},
		{"/.a/b", true, true},  // ".a" 不是 "." / ".a" is a legal segment
		{"/a.txt", true, true}, // 含点但无点段 / dots but no dot segments
		{"/a.txt/b.c", true, true},
		{"/...", true, true},
		{"/....", true, true},
		{"/a\x01b", true, false},
		{"/a\x7fb", true, false},
		{"a/b", true, false}, // 不以 '/' 开头 / must start with '/'
		{"", true, false},
	}
	for _, s := range samples {
		if got := validRequestPath(s.p, s.strict); got != s.want {
			t.Errorf("validRequestPath(%q, strict=%v) = %v, want %v", s.p, s.strict, got, s.want)
		}
		if got := validRequestPathRef(s.p, s.strict); got != s.want {
			t.Errorf("ref mismatch for %q: implementation under test changed semantics", s.p)
		}
	}
}

// FuzzValidRequestPathEquivalence compares the optimized scanner with the
// original segment-based implementation for arbitrary byte strings. The
// function intentionally treats paths as bytes, matching net/http's Path
// representation and the reference implementation.
func FuzzValidRequestPathEquivalence(f *testing.F) {
	for _, seed := range []string{
		"",
		"/",
		"/a/b",
		"/.",
		"/../x",
		"/a//b",
		"/a/",
		"/a\x00b",
		"//../",
		"/a.%ff/b",
	} {
		f.Add(seed, false)
		f.Add(seed, true)
	}

	f.Fuzz(func(t *testing.T, p string, strict bool) {
		if got, want := validRequestPath(p, strict), validRequestPathRef(p, strict); got != want {
			t.Fatalf("validRequestPath(%q, strict=%v) = %v, want %v", p, strict, got, want)
		}
	})
}
