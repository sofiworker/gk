//go:build ignore
// +build ignore

package main

import (
	"context"
	"encoding/xml"
	"log"
	"net/http"

	"github.com/sofiworker/gk/gws"
)

type echoRequest struct {
	XMLName xml.Name `xml:"urn:manual EchoRequest"`
	Message string   `xml:"message"`
}

type echoResponse struct {
	XMLName xml.Name `xml:"urn:manual EchoResponse"`
	Message string   `xml:"message"`
}

type echoService struct{}

func (echoService) Echo(ctx context.Context, req *echoRequest) (*echoResponse, error) {
	return &echoResponse{Message: "hello " + req.Message}, nil
}

func main() {
	desc := &gws.ServiceDesc{
		Name: "ManualEchoService",
		Operations: []gws.OperationDesc{
			{
				Operation: gws.Operation{
					Name:            "Echo",
					Action:          "urn:manual:Echo",
					RequestWrapper:  xml.Name{Space: "urn:manual", Local: "EchoRequest"},
					ResponseWrapper: xml.Name{Space: "urn:manual", Local: "EchoResponse"},
				},
				NewRequest:  func() any { return &echoRequest{} },
				NewResponse: func() any { return &echoResponse{} },
				Invoke: func(ctx context.Context, impl any, req any) (any, error) {
					return impl.(echoService).Echo(ctx, req.(*echoRequest))
				},
			},
		},
	}

	handler, err := gws.NewHandler(desc, echoService{})
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		log.Fatal(http.ListenAndServe(":8080", handler))
	}()

	client := gws.NewClient()
	req := gws.NewRequest(context.Background(), "http://127.0.0.1:8080", desc.Operations[0].Operation)
	req.SetHeader("X-Trace-ID", "trace-1")
	req.SetSOAPHeader(struct {
		XMLName xml.Name `xml:"urn:manual TraceHeader"`
		TraceID string   `xml:"trace_id"`
	}{TraceID: "trace-1"})
	req.SetBody(&echoRequest{Message: "soap"})

	var out echoResponse
	if err := client.Do(req, &out); err != nil {
		log.Fatal(err)
	}

	log.Printf("response=%s", out.Message)
}
