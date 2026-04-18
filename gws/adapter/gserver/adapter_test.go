package gserver

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpserver "github.com/sofiworker/gk/ghttp/gserver"
	"github.com/sofiworker/gk/gws"
)

const soapRequestXML = `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><Echo xmlns="urn:test"><value>hello</value></Echo></soapenv:Body></soapenv:Envelope>`

type ctxKey string

type mockInvoker func(ctx context.Context, operation string, req any) (any, error)

func (m mockInvoker) Invoke(ctx context.Context, operation string, req any) (any, error) {
	return m(ctx, operation, req)
}

func TestRegisterHandler(t *testing.T) {
	t.Run("invalid input", func(t *testing.T) {
		h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
		if err := Register(nil, "/ws", h); !errors.Is(err, ErrNilServer) {
			t.Fatalf("expected ErrNilServer, got %v", err)
		}

		s := httpserver.NewServer()
		if err := Register(s, "", h); !errors.Is(err, ErrEmptyPath) {
			t.Fatalf("expected ErrEmptyPath, got %v", err)
		}

		if err := Register(s, "/ws", nil); !errors.Is(err, ErrNilHandler) {
			t.Fatalf("expected ErrNilHandler, got %v", err)
		}
	})

	t.Run("bridge request and response", func(t *testing.T) {
		s := httpserver.NewServer()
		s.Use(func(c *httpserver.Context) {
			c.SetContext(context.WithValue(c.Context(), ctxKey("trace_id"), "trace-1"))
			c.Next()
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				t.Fatalf("unexpected method: %s", r.Method)
			}
			if r.Host != "example.com" {
				t.Fatalf("unexpected host: %s", r.Host)
			}
			if r.URL.Path != "/bridge" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if r.URL.RawQuery != "q=1&x=2" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			if r.Header.Get("X-Test") != "v1" {
				t.Fatalf("unexpected header: %s", r.Header.Get("X-Test"))
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != "payload" {
				t.Fatalf("unexpected body: %s", string(body))
			}
			if got := r.Context().Value(ctxKey("trace_id")); got != "trace-1" {
				t.Fatalf("unexpected context value: %v", got)
			}

			w.Header().Set("X-Handled", "yes")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("ok"))
		})

		if err := Register(s, "/bridge", handler); err != nil {
			t.Fatalf("Register failed: %v", err)
		}

		req := httptest.NewRequest(http.MethodDelete, "http://example.com/bridge?q=1&x=2", strings.NewReader("payload"))
		req.Header.Set("X-Test", "v1")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		if rec.Header().Get("X-Handled") != "yes" {
			t.Fatalf("unexpected response header: %s", rec.Header().Get("X-Handled"))
		}
		if rec.Body.String() != "ok" {
			t.Fatalf("unexpected response body: %s", rec.Body.String())
		}
	})

	t.Run("pass method through so wrapped handler returns 405", func(t *testing.T) {
		h, err := gws.NewHandler(&gws.ServiceDesc{}, nil)
		if err != nil {
			t.Fatalf("NewHandler failed: %v", err)
		}

		s := httpserver.NewServer()
		if err := Register(s, "/ws", h); err != nil {
			t.Fatalf("Register failed: %v", err)
		}

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "http://example.com/ws", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 from wrapped handler, got %d", rec.Code)
		}
	})
}

func TestRegisterInvalidPath(t *testing.T) {
	s := httpserver.NewServer()
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	cases := []struct {
		name string
		path string
	}{
		{
			name: "without leading slash",
			path: "ws",
		},
		{
			name: "contains query separator",
			path: "/ws?x=1",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("Register should not panic, panic=%v", p)
				}
			}()

			err := Register(s, tc.path, h)
			if !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("expected ErrInvalidPath, got %v", err)
			}
		})
	}
}

func TestRegisterInvalidWildcardPaths(t *testing.T) {
	s := httpserver.NewServer()
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	cases := []struct {
		name string
		path string
	}{
		{
			name: "named param missing",
			path: "/:",
		},
		{
			name: "wildcard missing",
			path: "/*",
		},
		{
			name: "wildcard not at end",
			path: "/a/*x/b",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("Register should not panic, panic=%v", p)
				}
			}()

			err := Register(s, tc.path, h)
			if !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("expected ErrInvalidPath, got %v", err)
			}
		})
	}
}

func TestRegisterRequestProjection(t *testing.T) {
	s := httpserver.NewServer()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Scheme != "http" {
			t.Fatalf("unexpected scheme: %q", r.URL.Scheme)
		}
		if r.URL.Host != "example.com" {
			t.Fatalf("unexpected url host: %q", r.URL.Host)
		}
		if r.Proto != "HTTP/1.1" {
			t.Fatalf("unexpected proto: %q", r.Proto)
		}
		if r.ProtoMajor != 1 || r.ProtoMinor != 1 {
			t.Fatalf("unexpected proto version: %d.%d", r.ProtoMajor, r.ProtoMinor)
		}
		if r.RemoteAddr != "10.0.0.1:8080" {
			t.Fatalf("unexpected remote addr: %q", r.RemoteAddr)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := Register(s, "/projection", handler); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://example.com/projection", nil)
	req.RemoteAddr = "10.0.0.1:8080"

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
}

func TestRegisterServeWSDL(t *testing.T) {
	h, err := gws.NewHandler(&gws.ServiceDesc{
		WSDL: &gws.WSDLAssetSet{
			Main: []byte("<definitions/>"),
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	s := httpserver.NewServer()
	if err := Register(s, "/ws", h); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com/ws?wsdl", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "<definitions/>" {
		t.Fatalf("unexpected wsdl body: %s", got)
	}
}

func TestRegisterServeSOAP(t *testing.T) {
	h, err := gws.NewHandler(&gws.ServiceDesc{
		Operations: []gws.OperationDesc{{
			Operation: gws.Operation{
				Name:            "Echo",
				RequestWrapper:  xml.Name{Space: "urn:test", Local: "Echo"},
				ResponseWrapper: xml.Name{Space: "urn:test", Local: "EchoResponse"},
			},
			NewRequest: func() any {
				return &struct {
					XMLName xml.Name `xml:"urn:test Echo"`
					Value   string   `xml:"value"`
				}{}
			},
		}},
	}, mockInvoker(func(ctx context.Context, operation string, req any) (any, error) {
		in, ok := req.(*struct {
			XMLName xml.Name `xml:"urn:test Echo"`
			Value   string   `xml:"value"`
		})
		if !ok {
			t.Fatalf("unexpected req type: %T", req)
		}
		if in.Value != "hello" {
			t.Fatalf("unexpected request value: %q", in.Value)
		}

		return &struct {
			XMLName xml.Name `xml:"urn:test EchoResponse"`
			Value   string   `xml:"value"`
		}{Value: "ok"}, nil
	}))
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	s := httpserver.NewServer()
	if err := Register(s, "/ws", h); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "http://example.com/ws", strings.NewReader(soapRequestXML)))
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<EchoResponse xmlns=\"urn:test\">") {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}
