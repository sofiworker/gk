package gws

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const soapRequestXML = `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><Echo xmlns="urn:test"><value>hello</value></Echo></soapenv:Body></soapenv:Envelope>`

type mockInvoker func(ctx context.Context, operation string, req any) (any, error)

func (m mockInvoker) Invoke(ctx context.Context, operation string, req any) (any, error) {
	return m(ctx, operation, req)
}

func TestHandlerDispatchOperation(t *testing.T) {
	h, err := NewHandler(&ServiceDesc{
		Operations: []OperationDesc{{
			Operation: Operation{
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

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/ws", strings.NewReader(soapRequestXML)))
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), "<EchoResponse xmlns=\"urn:test\">") {
		t.Fatalf("unexpected response body: %s", resp.Body.String())
	}
}

func TestHandlerServeWSDL(t *testing.T) {
	h, err := NewHandler(&ServiceDesc{
		WSDL: &WSDLAssetSet{
			Main: []byte("<definitions/>"),
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/ws?wsdl", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
	if got := strings.TrimSpace(resp.Body.String()); got != "<definitions/>" {
		t.Fatalf("unexpected wsdl body: %q", got)
	}
}

func TestHandlerServeXSD(t *testing.T) {
	h, err := NewHandler(&ServiceDesc{
		WSDL: &WSDLAssetSet{
			Main: []byte("<definitions/>"),
			XSD: map[string][]byte{
				"types.xsd": []byte("<schema/>"),
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/ws?xsd=types.xsd", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
	if got := strings.TrimSpace(resp.Body.String()); got != "<schema/>" {
		t.Fatalf("unexpected xsd body: %q", got)
	}
}

func TestHandlerFaultResponse(t *testing.T) {
	t.Run("parse error", func(t *testing.T) {
		h, err := NewHandler(&ServiceDesc{
			Operations: []OperationDesc{{
				Operation: Operation{
					Name:           "Echo",
					RequestWrapper: xml.Name{Space: "urn:test", Local: "Echo"},
				},
			}},
		}, nil)
		if err != nil {
			t.Fatalf("NewHandler failed: %v", err)
		}

		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/ws", strings.NewReader("<bad-xml")))
		if resp.Code != http.StatusInternalServerError {
			t.Fatalf("unexpected status: %d", resp.Code)
		}
		body := resp.Body.String()
		if !strings.Contains(body, "<faultcode>soap:Client</faultcode>") {
			t.Fatalf("unexpected fault body: %s", body)
		}
	})

	t.Run("operation not found", func(t *testing.T) {
		h, err := NewHandler(&ServiceDesc{
			Operations: []OperationDesc{{
				Operation: Operation{
					Name:           "Echo",
					RequestWrapper: xml.Name{Space: "urn:test", Local: "Echo"},
				},
			}},
		}, nil)
		if err != nil {
			t.Fatalf("NewHandler failed: %v", err)
		}

		reqXML := `<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"><soapenv:Body><Unknown xmlns="urn:test"/></soapenv:Body></soapenv:Envelope>`
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/ws", strings.NewReader(reqXML)))
		if resp.Code != http.StatusInternalServerError {
			t.Fatalf("unexpected status: %d", resp.Code)
		}
		body := resp.Body.String()
		if !strings.Contains(body, "<faultstring>operation not found</faultstring>") {
			t.Fatalf("unexpected fault body: %s", body)
		}
	})

	t.Run("invoke returns fault", func(t *testing.T) {
		h, err := NewHandler(&ServiceDesc{
			Operations: []OperationDesc{{
				Operation: Operation{
					Name:            "Echo",
					RequestWrapper:  xml.Name{Space: "urn:test", Local: "Echo"},
					ResponseWrapper: xml.Name{Space: "urn:test", Local: "EchoResponse"},
				},
				NewRequest: func() any {
					return &struct {
						XMLName xml.Name `xml:"urn:test Echo"`
					}{}
				},
			}},
		}, mockInvoker(func(ctx context.Context, operation string, req any) (any, error) {
			return nil, &Fault{
				Code:   "soap:Client",
				String: "bad request",
			}
		}))
		if err != nil {
			t.Fatalf("NewHandler failed: %v", err)
		}

		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/ws", strings.NewReader(soapRequestXML)))
		if resp.Code != http.StatusInternalServerError {
			t.Fatalf("unexpected status: %d", resp.Code)
		}
		body := resp.Body.String()
		if !strings.Contains(body, "<faultstring>bad request</faultstring>") {
			t.Fatalf("unexpected fault body: %s", body)
		}
	})

	t.Run("invoke returns generic error", func(t *testing.T) {
		h, err := NewHandler(&ServiceDesc{
			Operations: []OperationDesc{{
				Operation: Operation{
					Name:            "Echo",
					RequestWrapper:  xml.Name{Space: "urn:test", Local: "Echo"},
					ResponseWrapper: xml.Name{Space: "urn:test", Local: "EchoResponse"},
				},
				NewRequest: func() any {
					return &struct {
						XMLName xml.Name `xml:"urn:test Echo"`
					}{}
				},
			}},
		}, mockInvoker(func(ctx context.Context, operation string, req any) (any, error) {
			return nil, errors.New("boom")
		}))
		if err != nil {
			t.Fatalf("NewHandler failed: %v", err)
		}

		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/ws", strings.NewReader(soapRequestXML)))
		if resp.Code != http.StatusInternalServerError {
			t.Fatalf("unexpected status: %d", resp.Code)
		}
		body := resp.Body.String()
		if !strings.Contains(body, "<faultstring>boom</faultstring>") {
			t.Fatalf("unexpected fault body: %s", body)
		}
	})
}

func TestHandlerMethodNotAllowed(t *testing.T) {
	h, err := NewHandler(&ServiceDesc{}, nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPut, "/ws", nil))
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
}

func TestHandlerResponseWrapperMismatch(t *testing.T) {
	h, err := NewHandler(&ServiceDesc{
		Operations: []OperationDesc{{
			Operation: Operation{
				Name:            "Echo",
				RequestWrapper:  xml.Name{Space: "urn:test", Local: "Echo"},
				ResponseWrapper: xml.Name{Space: "urn:test", Local: "EchoResponse"},
			},
			NewRequest: func() any {
				return &struct {
					XMLName xml.Name `xml:"urn:test Echo"`
				}{}
			},
		}},
	}, mockInvoker(func(ctx context.Context, operation string, req any) (any, error) {
		return &struct {
			XMLName xml.Name `xml:"urn:test WrongResponse"`
			Value   string   `xml:"value"`
		}{Value: "bad"}, nil
	}))
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/ws", strings.NewReader(soapRequestXML)))
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
	if strings.Contains(resp.Body.String(), "<WrongResponse") {
		t.Fatalf("should not write successful payload on mismatch: %s", resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), ErrResponseWrapperMismatch.Error()) {
		t.Fatalf("unexpected fault body: %s", resp.Body.String())
	}
}

func TestHandlerMissingRequestFactory(t *testing.T) {
	called := false
	h, err := NewHandler(&ServiceDesc{
		Operations: []OperationDesc{{
			Operation: Operation{
				Name:           "Echo",
				RequestWrapper: xml.Name{Space: "urn:test", Local: "Echo"},
			},
			NewRequest: nil,
		}},
	}, mockInvoker(func(ctx context.Context, operation string, req any) (any, error) {
		called = true
		return &struct {
			XMLName xml.Name `xml:"urn:test EchoResponse"`
		}{}, nil
	}))
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/ws", strings.NewReader(soapRequestXML)))
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
	if called {
		t.Fatal("business invoker should not be called without request factory")
	}
	if !strings.Contains(resp.Body.String(), ErrMissingRequestFactory.Error()) {
		t.Fatalf("unexpected fault body: %s", resp.Body.String())
	}
}

func TestHandlerMissingOperationInvoker(t *testing.T) {
	h, err := NewHandler(&ServiceDesc{
		Operations: []OperationDesc{{
			Operation: Operation{
				Name:           "Echo",
				RequestWrapper: xml.Name{Space: "urn:test", Local: "Echo"},
			},
			NewRequest: func() any {
				return &struct {
					XMLName xml.Name `xml:"urn:test Echo"`
				}{}
			},
		}},
	}, struct{}{})
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/ws", strings.NewReader(soapRequestXML)))
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), ErrOperationInvokerNotFound.Error()) {
		t.Fatalf("unexpected fault body: %s", resp.Body.String())
	}
}
