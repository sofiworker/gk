// Package replay 提供流量回放：读取 pcap/pcapng 报文并按原始时间戳节奏
// （或 pps/倍速）注入目标。IP+MAC 重写使用 RFC 1624 增量校验和更新，
// 无需重编码整个报文。设计见
// docs/superpowers/specs/2026-08-16-gnet-network-foundation-redesign.md §5.6。
//
// Package replay provides traffic replay: packets read from pcap/pcapng are
// injected at the original timestamp pacing (or by pps/multiplier). IP+MAC
// rewriting uses RFC 1624 incremental checksum updates, avoiding full
// re-encoding. See the gnet redesign spec §5.6.
package replay

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/sofiworker/gk/gnet/craft"
	"github.com/sofiworker/gk/gnet/packet"
)

// Source 是报文来源；流尽返回 io.EOF。与 sniff.Source 同形，实现可互用。
// Source is a packet origin; exhaustion returns io.EOF. It is structurally
// identical to sniff.Source, so implementations interoperate.
type Source interface {
	Next() (*packet.Packet, error)
}

// Config 是回放配置。
// Config holds replay configuration.
type Config struct {
	// Pacing 固定每秒注入包数（>0 时覆盖时间戳节奏）。
	Pacing int
	// Multiplier 相对原始时间戳节奏的倍速（>0，默认 1）。
	Multiplier float64
	// Loop 循环次数；-1 无限循环，0 表示单次。
	Loop int
	// Rewrite 非零时重写源/目的 IP 与 MAC。
	Rewrite RewriteConfig
}

// RewriteConfig 是报文重写参数。
// RewriteConfig holds packet rewrite parameters.
type RewriteConfig struct {
	SrcIP, DstIP   string // IPv4 字面量
	SrcMAC, DstMAC string // 6 字节 MAC 字面量
}

// Option 配置回放器。
// Option configures the replayer.
type Option func(*Config)

// WithPacing 设置固定每秒包数（覆盖时间戳节奏）。
// WithPacing sets a fixed packets-per-second rate (overrides timestamps).
func WithPacing(pps int) Option {
	return func(c *Config) { c.Pacing = pps }
}

// WithMultiplier 设置相对原始节奏的倍速。
// WithMultiplier sets the speed multiplier over the original pacing.
func WithMultiplier(n float64) Option {
	return func(c *Config) {
		if n > 0 {
			c.Multiplier = n
		}
	}
}

// WithLoop 设置循环次数（-1 无限）。
// WithLoop sets the loop count (-1 infinite).
func WithLoop(n int) Option {
	return func(c *Config) { c.Loop = n }
}

// WithRewrite 设置 IP+MAC 重写。
// WithRewrite sets IP+MAC rewriting.
func WithRewrite(srcIP, dstIP, srcMAC, dstMAC string) Option {
	return func(c *Config) {
		c.Rewrite = RewriteConfig{SrcIP: srcIP, DstIP: dstIP, SrcMAC: srcMAC, DstMAC: dstMAC}
	}
}

// Stats 是回放统计。
// Stats holds replay statistics.
type Stats struct {
	Packets int64
	Bytes   int64
	Loops   int
	Elapsed time.Duration
}

// Replayer 是回放器。
// Replayer is the replayer.
type Replayer struct {
	src     Source
	dst     craft.Injector
	cfg     Config
	rewrite *rewriter
	stats   Stats
}

// New 创建回放器；src 流尽后按 Loop 配置决定是否重读（Loop<0 仅支持
// 可重读来源，SliceSource 类来源需调用方重置）。
//
// New creates a replayer; on source exhaustion the Loop option decides
// whether to replay again (Loop<0 requires a re-readable source; callers
// must reset slice-like sources).
func New(src Source, dst craft.Injector, opts ...Option) (*Replayer, error) {
	cfg := Config{Multiplier: 1, Loop: 1}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.Multiplier <= 0 {
		return nil, fmt.Errorf("replay: multiplier must be positive")
	}
	r := &Replayer{src: src, dst: dst, cfg: cfg}
	if cfg.Rewrite.SrcIP != "" || cfg.Rewrite.DstIP != "" || cfg.Rewrite.SrcMAC != "" || cfg.Rewrite.DstMAC != "" {
		rw, err := newRewriter(cfg.Rewrite)
		if err != nil {
			return nil, err
		}
		r.rewrite = rw
	}
	return r, nil
}

// Stats 返回当前统计。
// Stats returns the current stats.
func (r *Replayer) Stats() Stats { return r.stats }

// Run 执行回放直至 ctx 取消或源流尽。
// Run replays until ctx cancellation or source exhaustion.
func (r *Replayer) Run(ctx context.Context) error {
	start := time.Now()
	for {
		before := r.stats.Packets
		done, err := r.runOnce(ctx, start)
		if err != nil {
			return err
		}
		r.stats.Loops++
		if r.cfg.Loop >= 0 && r.stats.Loops >= r.cfg.Loop {
			break
		}
		// 源流尽且本轮未读到包：来源不可重读，终止循环。
		if done && r.stats.Packets == before {
			break
		}
	}
	return nil
}

// runOnce 回放一轮；返回源是否流尽。
// runOnce replays one pass; returns whether the source is exhausted.
func (r *Replayer) runOnce(ctx context.Context, start time.Time) (bool, error) {
	var (
		prevTS time.Time
		next   = start
	)
	for {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		default:
		}
		pkt, err := r.src.Next()
		if err == io.EOF {
			return true, nil
		}
		if err != nil {
			return true, err
		}
		if r.cfg.Pacing > 0 {
			next = start.Add(time.Duration(r.stats.Packets+1) * time.Second / time.Duration(r.cfg.Pacing))
		} else if !prevTS.IsZero() && !pkt.Timestamp.IsZero() {
			gap := pkt.Timestamp.Sub(prevTS)
			if gap > 0 {
				gap = time.Duration(float64(gap) / r.cfg.Multiplier)
				next = next.Add(gap)
			}
		}
		prevTS = pkt.Timestamp
		wait := time.Until(next)
		if wait > 0 {
			select {
			case <-ctx.Done():
				return true, ctx.Err()
			case <-time.After(wait):
			}
		}
		out := pkt.Data
		if r.rewrite != nil {
			out, err = r.rewrite.rewrite(pkt.Data)
			if err != nil {
				return false, err
			}
		}
		if err := r.dst.Inject(out); err != nil {
			return false, err
		}
		r.stats.Packets++
		r.stats.Bytes += int64(len(out))
	}
}
