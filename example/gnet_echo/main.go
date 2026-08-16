// gnet_echo 演示 gnet reactor 的最小回显服务器：
// 事件回调式 API、连接计数与 SIGINT 优雅退出。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sofiworker/gk/gnet"
)

type echoServer struct {
	conns atomic.Int64
}

func (e *echoServer) OnBoot(_ *gnet.Server) error {
	fmt.Println("gnet echo server booted")
	return nil
}

func (e *echoServer) OnOpen(c gnet.Conn) ([]byte, gnet.Action) {
	e.conns.Add(1)
	fmt.Printf("[open] id=%d remote=%s\n", c.ID(), c.RemoteAddr())
	return nil, gnet.ActionNone
}

func (e *echoServer) OnTraffic(c gnet.Conn) gnet.Action {
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		return gnet.ActionClose
	}
	if _, err := c.Write(buf[:n]); err != nil {
		return gnet.ActionClose
	}
	return gnet.ActionNone
}

func (e *echoServer) OnClose(c gnet.Conn, err error) {
	e.conns.Add(-1)
	fmt.Printf("[close] id=%d err=%v\n", c.ID(), err)
}

func (e *echoServer) OnTick() (time.Duration, gnet.Action) {
	fmt.Printf("[tick] active conns: %d\n", e.conns.Load())
	return time.Second, gnet.ActionNone
}

func main() {
	srv := gnet.New(&echoServer{})

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Println("stopping...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Stop(ctx); err != nil {
			fmt.Printf("stop: %v\n", err)
		}
	}()

	fmt.Println("listening on :9000")
	if err := srv.Serve(":9000"); err != nil {
		fmt.Printf("serve: %v\n", err)
		os.Exit(1)
	}
}
