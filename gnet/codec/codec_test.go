package codec

import (
	"bytes"
	"errors"
	"testing"
)

// TestLengthFieldFramerRoundTrip 覆盖 Encode→Split 对称与 Netty 参数语义。
func TestLengthFieldFramerRoundTrip(t *testing.T) {
	f := LengthFieldFramer{LengthFieldOffset: 2, LengthFieldLength: 4, LengthAdjustment: 0}
	msg := []byte("hello world")
	encoded, err := f.Encode(msg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	adv, frame, need, err := f.Split(encoded, false)
	if err != nil || need != 0 || adv != len(encoded) {
		t.Fatalf("Split = (%d, %d, %v), want (%d, 0, nil)", adv, need, err, len(encoded))
	}
	got := frame[f.LengthFieldOffset+f.LengthFieldLength:]
	if !bytes.Equal(got, msg) {
		t.Fatalf("payload = %q, want %q", got, msg)
	}
}

// TestLengthFieldFramerAdjustmentAndStrip 覆盖 LengthAdjustment 与剥头。
func TestLengthFieldFramerAdjustmentAndStrip(t *testing.T) {
	f := LengthFieldFramer{
		LengthFieldOffset:   0,
		LengthFieldLength:   2,
		LengthAdjustment:    4, // 长度字段计 4 字节尾部（如 CRC）
		InitialBytesToStrip: 2, // 剥掉长度字段
	}
	msg := []byte("abc")
	encoded, err := f.Encode(msg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) != 2+3 {
		t.Fatalf("encoded len = %d, want 5 (length field excludes adjustment)", len(encoded))
	}
	adv, frame, need, err := f.Split(append(encoded, 'C', 'R', 'C', '!'), false)
	if err != nil || need != 0 {
		t.Fatalf("Split err=%v need=%d", err, need)
	}
	if adv != len(encoded)+4 {
		t.Fatalf("adv = %d, want %d (includes adjustment)", adv, len(encoded)+4)
	}
	// strip 只剥头部 2 字节；adjustment 区域（CRC）属于帧体。
	// strip removes only the 2-byte header; the adjustment region (CRC)
	// remains part of the frame.
	if !bytes.Equal(frame, []byte("abcCRC!")) {
		t.Fatalf("frame = %q, want abcCRC! (header stripped)", frame)
	}
}

// TestLengthFieldFramerPartial 覆盖半包与 atEOF。
func TestLengthFieldFramerPartial(t *testing.T) {
	f := LengthFieldFramer{LengthFieldLength: 2}
	encoded, _ := f.Encode([]byte("payload"))
	// 逐字节喂入。
	var acc []byte
	for i := 0; i < len(encoded)-1; i++ {
		acc = append(acc, encoded[i])
		adv, frame, need, err := f.Split(acc, false)
		if frame != nil || err != nil {
			t.Fatalf("partial split at %d: frame=%v err=%v", i, frame, err)
		}
		_ = adv
		if need <= 0 {
			t.Fatalf("need = %d, want > 0 at %d", need, i)
		}
	}
	adv, frame, need, err := f.Split(encoded, false)
	if err != nil || need != 0 || adv != len(encoded) {
		t.Fatalf("full split = (%d,%d,%v)", adv, need, err)
	}
	if !bytes.Equal(frame[2:], []byte("payload")) {
		t.Fatalf("payload = %q", frame[2:])
	}
	// atEOF 半包。
	if _, _, _, err := f.Split(encoded[:len(encoded)-1], true); !errors.Is(err, ErrUnexpectedEOF) {
		t.Fatalf("atEOF partial err = %v, want ErrUnexpectedEOF", err)
	}
}

// TestLengthFieldFramerMax 覆盖 MaxFrameLength 与非法宽度。
func TestLengthFieldFramerMax(t *testing.T) {
	f := LengthFieldFramer{LengthFieldLength: 2, MaxFrameLength: 4}
	encoded, _ := f.Encode([]byte("toolong"))
	if _, _, _, err := f.Split(encoded, false); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v, want ErrFrameTooLarge", err)
	}
	bad := LengthFieldFramer{LengthFieldLength: 3}
	if _, err := bad.Encode([]byte("x")); err == nil {
		t.Fatal("Encode with 3-byte length field = nil error")
	}
}

// TestDelimiterFramer 覆盖分隔符分帧与 EOF 尾帧。
func TestDelimiterFramer(t *testing.T) {
	f := DelimiterFramer{Delim: '|'}
	encoded, _ := f.Encode([]byte("msg"))
	if string(encoded) != "msg|" {
		t.Fatalf("Encode = %q, want msg|", encoded)
	}
	adv, frame, need, err := f.Split([]byte("a|b|c"), false)
	if err != nil || need != 0 || adv != 2 || string(frame) != "a" {
		t.Fatalf("Split = (%d, %q, %d, %v)", adv, frame, need, err)
	}
	adv, frame, _, _ = f.Split([]byte("abc"), true)
	if adv != 3 || string(frame) != "abc" {
		t.Fatalf("EOF tail = (%d, %q)", adv, frame)
	}
}

// TestLineFramer 覆盖 CRLF 行与 EOF 尾行。
func TestLineFramer(t *testing.T) {
	f := LineFramer{}
	encoded, _ := f.Encode([]byte("hi"))
	if string(encoded) != "hi\r\n" {
		t.Fatalf("Encode = %q", encoded)
	}
	adv, frame, _, err := f.Split([]byte("hello\r\nworld\n"), false)
	if err != nil || adv != 7 || string(frame) != "hello" {
		t.Fatalf("Split = (%d, %q, %v)", adv, frame, err)
	}
	adv, frame, _, _ = f.Split([]byte("tail\r"), true)
	if string(frame) != "tail" || adv != 5 {
		t.Fatalf("EOF tail = (%d, %q)", adv, frame)
	}
}

// TestFixedLengthFramer 覆盖定长分帧。
func TestFixedLengthFramer(t *testing.T) {
	f := FixedLengthFramer{N: 4}
	encoded, _ := f.Encode([]byte("ab"))
	if len(encoded) != 4 || string(encoded) != "ab\x00\x00" {
		t.Fatalf("Encode = %q, want zero-padded 4 bytes", encoded)
	}
	adv, frame, need, err := f.Split([]byte("abcdefgh"), false)
	if err != nil || need != 0 || adv != 4 || string(frame) != "abcd" {
		t.Fatalf("Split = (%d, %q, %d, %v)", adv, frame, need, err)
	}
	if _, _, _, err := f.Split([]byte("ab"), true); !errors.Is(err, ErrUnexpectedEOF) {
		t.Fatalf("atEOF partial err = %v", err)
	}
}

// TestHTTPFramerContentLength 覆盖 Content-Length 消息（跨调用累积）。
func TestHTTPFramerContentLength(t *testing.T) {
	f := &HTTPFramer{}
	req := "POST /x HTTP/1.1\r\nHost: h\r\nContent-Length: 5\r\n\r\nhello"
	// 逐段喂入。
	var got []byte
	for i := 1; i <= len(req); i++ {
		adv, frame, _, err := f.Split([]byte(req[i-1:i]), false)
		if err != nil {
			t.Fatalf("feed@%d: %v", i, err)
		}
		if adv != 1 {
			t.Fatalf("adv = %d, want 1", adv)
		}
		if frame != nil {
			got = append([]byte(nil), frame...)
			break
		}
	}
	if string(got) != req {
		t.Fatalf("message = %q, want %q", got, req)
	}
}

// TestHTTPFramerChunked 覆盖 chunked 消息。
func TestHTTPFramerChunked(t *testing.T) {
	f := &HTTPFramer{}
	req := "POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n" +
		"5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n"
	adv, frame, need, err := f.Split([]byte(req), false)
	if err != nil || need != 0 {
		t.Fatalf("Split err=%v need=%d", err, need)
	}
	if adv != len(req) || string(frame) != req {
		t.Fatalf("frame mismatch: adv=%d frame=%q", adv, frame)
	}
	// 连续第二条消息。
	adv, frame, _, err = f.Split([]byte("GET / HTTP/1.1\r\n\r\n"), false)
	if err != nil || adv != 18 || len(frame) != 18 {
		t.Fatalf("second message = (%d, %v)", adv, err)
	}
}

// TestHTTPFramerErrors 覆盖无定界（按无体处理）与 atEOF 错误。
func TestHTTPFramerErrors(t *testing.T) {
	f := &HTTPFramer{}
	// 无 Content-Length/Transfer-Encoding：按无请求体处理。
	adv, frame, _, err := f.Split([]byte("GET / HTTP/1.1\r\n\r\n"), false)
	if err != nil || frame == nil || adv != 18 {
		t.Fatalf("bodyless message = (%d, %v)", adv, err)
	}
	f2 := &HTTPFramer{}
	if _, _, _, err := f2.Split([]byte("GET / HTTP/1.1\r\nContent-Length: 10\r\n\r\nabc"), true); !errors.Is(err, ErrUnexpectedEOF) {
		t.Fatalf("atEOF partial err = %v", err)
	}
}

// TestHTTPFramerRoundTrip 覆盖 Encode 透传 + Split 还原。
func TestHTTPFramerRoundTrip(t *testing.T) {
	f := &HTTPFramer{}
	req := []byte("GET / HTTP/1.1\r\nHost: x\r\nContent-Length: 0\r\n\r\n")
	encoded, err := f.Encode(req)
	if err != nil || !bytes.Equal(encoded, req) {
		t.Fatalf("Encode = %q, %v", encoded, err)
	}
	adv, frame, _, err := f.Split(encoded, false)
	if err != nil || adv != len(req) || !bytes.Equal(frame, req) {
		t.Fatalf("Split = (%d, %v)", adv, err)
	}
}
