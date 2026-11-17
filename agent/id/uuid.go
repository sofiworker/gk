// Copyright 2023 Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package id 为 gai 生成 UUID v7，不改变外部模型提供的关联 ID。
// Package id generates UUID v7 identifiers for gai without replacing external model correlation IDs.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

var (
	timeMu     sync.Mutex
	lastV7Time int64
)

// NewV7 返回标准小写 UUID v7；时间序列在进程内按生成临界区顺序递增。
// NewV7 returns a canonical lowercase UUID v7 with process-local ordering at the generation critical section.
// ID 不替代消息序号或 CAS 版本；跨进程和并发返回顺序没有保证。
// IDs do not replace message sequences or CAS versions; cross-process and concurrent return ordering is not guaranteed.
func NewV7() string {
	var value [16]byte
	rand.Read(value[:])
	timeMu.Lock()
	timestamp := nextV7Time(time.Now().UnixNano(), lastV7Time)
	lastV7Time = timestamp
	timeMu.Unlock()
	milli, seq := timestamp>>12, timestamp&0xfff
	value[0] = byte(milli >> 40)
	value[1] = byte(milli >> 32)
	value[2] = byte(milli >> 24)
	value[3] = byte(milli >> 16)
	value[4] = byte(milli >> 8)
	value[5] = byte(milli)
	value[6] = 0x70 | byte(seq>>8)
	value[7] = byte(seq)
	value[8] = 0x80 | (value[8] & 0x3f)
	var out [36]byte
	hex.Encode(out[0:8], value[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], value[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], value[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], value[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], value[10:16])
	return string(out[:])
}

// 时间回退或同刻重复时推进逻辑序列；溢出自然推进到下一逻辑毫秒。
// Advance the logical sequence on clock rollback or ties; overflow advances to the next logical millisecond.
func nextV7Time(nano, previous int64) int64 {
	const nanoPerMilli = 1000000
	milli := nano / nanoPerMilli
	seq := (nano - milli*nanoPerMilli) >> 8
	next := milli<<12 + seq
	if next <= previous {
		next = previous + 1
	}
	return next
}
