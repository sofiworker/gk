package gws_test

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"

	"github.com/sofiworker/gk/gws"
)

type manualEchoRequest struct {
	XMLName xml.Name `xml:"urn:manual EchoRequest"`
	Message string   `xml:"message"`
}

type manualEchoResponse struct {
	XMLName xml.Name `xml:"urn:manual EchoResponse"`
	Message string   `xml:"message"`
}

type manualEchoService struct{}

func (manualEchoService) Echo(ctx context.Context, req *manualEchoRequest) (*manualEchoResponse, error) {
	return &manualEchoResponse{Message: "hello " + req.Message}, nil
}

type manualFaultService struct{}

func (manualFaultService) Echo(ctx context.Context, req *manualEchoRequest) (*manualEchoResponse, error) {
	return nil, &gws.Fault{
		Code:   "soap:Client",
		String: "invalid message",
		Detail: struct {
			XMLName xml.Name `xml:"urn:manual ValidationError"`
			Field   string   `xml:"field"`
		}{Field: "message"},
	}
}

type manualEchoInvoker interface {
	Echo(ctx context.Context, req *manualEchoRequest) (*manualEchoResponse, error)
}

func manualEchoDesc() *gws.ServiceDesc {
	return &gws.ServiceDesc{
		Name: "ManualEchoService",
		Operations: []gws.OperationDesc{
			{
				Operation: gws.Operation{
					Name:            "Echo",
					Action:          "urn:manual:Echo",
					RequestWrapper:  xml.Name{Space: "urn:manual", Local: "EchoRequest"},
					ResponseWrapper: xml.Name{Space: "urn:manual", Local: "EchoResponse"},
				},
				NewRequest:  func() any { return &manualEchoRequest{} },
				NewResponse: func() any { return &manualEchoResponse{} },
				Invoke: func(ctx context.Context, impl any, req any) (any, error) {
					return impl.(manualEchoInvoker).Echo(ctx, req.(*manualEchoRequest))
				},
			},
		},
	}
}

func Example_manualClientAndHandler() {
	serviceDesc := manualEchoDesc()

	handler, err := gws.NewHandler(serviceDesc, manualEchoService{})
	if err != nil {
		panic(err)
	}

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := gws.NewClient()
	req := gws.NewRequest(context.Background(), srv.URL, serviceDesc.Operations[0].Operation)
	req.SetSOAPHeader(struct {
		XMLName xml.Name `xml:"urn:manual TraceHeader"`
		TraceID string   `xml:"trace_id"`
	}{TraceID: "trace-1"})
	req.SetBody(&manualEchoRequest{Message: "soap"})

	var out manualEchoResponse
	if err := client.Do(req, &out); err != nil {
		panic(err)
	}

	fmt.Println(out.Message)
	// Output: hello soap
}

func Example_manualEnvelopeAndRawResponse() {
	serviceDesc := manualEchoDesc()

	handler, err := gws.NewHandler(serviceDesc, manualEchoService{})
	if err != nil {
		panic(err)
	}

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := gws.NewClient()
	req := gws.NewRequest(context.Background(), srv.URL, serviceDesc.Operations[0].Operation)
	req.SetEnvelope(gws.Envelope{
		Namespace: gws.SOAP11EnvelopeNamespace,
		Header: &gws.Header{
			Content: struct {
				XMLName xml.Name `xml:"urn:manual TraceHeader"`
				TraceID string   `xml:"trace_id"`
			}{TraceID: "trace-1"},
		},
		Body: gws.Body{
			Content: &manualEchoRequest{Message: "soap"},
		},
	})

	rawXML, err := client.DoRaw(req)
	if err != nil {
		panic(err)
	}

	var out manualEchoResponse
	if err := gws.UnmarshalBody(rawXML, serviceDesc.Operations[0].Operation.ResponseWrapper, &out); err != nil {
		panic(err)
	}

	fmt.Println(out.Message)
	// Output: hello soap
}

func Example_manualFaultDetail() {
	serviceDesc := manualEchoDesc()

	handler, err := gws.NewHandler(serviceDesc, manualFaultService{})
	if err != nil {
		panic(err)
	}

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := gws.NewClient()
	req := gws.NewRequest(context.Background(), srv.URL, serviceDesc.Operations[0].Operation)
	req.SetBody(&manualEchoRequest{Message: ""})

	err = client.Do(req, &manualEchoResponse{})
	var faultErr *gws.FaultError
	if !errors.As(err, &faultErr) {
		panic(err)
	}

	detail, _ := faultErr.Fault.Detail.(string)
	fmt.Println(faultErr.Fault.Code, strings.Contains(detail, "ValidationError"))
	// Output: soap:Client true
}
