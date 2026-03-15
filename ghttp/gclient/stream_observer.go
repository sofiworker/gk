package gclient

import (
	"net/http"
	"time"
)

type StreamProtocol string

const (
	StreamProtocolSSE       StreamProtocol = "sse"
	StreamProtocolWebSocket StreamProtocol = "websocket"
)

type StreamConnectInfo struct {
	Protocol StreamProtocol
	URL      string
	Response *http.Response
}

type StreamRetryInfo struct {
	Protocol StreamProtocol
	URL      string
	Attempt  int
	Delay    time.Duration
	Err      error
}

type StreamCloseInfo struct {
	Protocol StreamProtocol
	URL      string
	Err      error
}

type StreamObserver interface {
	OnConnect(StreamConnectInfo) error
	OnRetry(StreamRetryInfo)
	OnError(StreamProtocol, string, error)
	OnClose(StreamCloseInfo)
}

type StreamObserverFuncs struct {
	Connect func(StreamConnectInfo) error
	Retry   func(StreamRetryInfo)
	Error   func(StreamProtocol, string, error)
	Close   func(StreamCloseInfo)
}

func (s StreamObserverFuncs) OnConnect(info StreamConnectInfo) error {
	if s.Connect != nil {
		return s.Connect(info)
	}
	return nil
}

func (s StreamObserverFuncs) OnRetry(info StreamRetryInfo) {
	if s.Retry != nil {
		s.Retry(info)
	}
}

func (s StreamObserverFuncs) OnError(protocol StreamProtocol, url string, err error) {
	if s.Error != nil {
		s.Error(protocol, url, err)
	}
}

func (s StreamObserverFuncs) OnClose(info StreamCloseInfo) {
	if s.Close != nil {
		s.Close(info)
	}
}
