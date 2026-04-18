package gws

import (
	"context"
	"encoding/xml"
	"errors"
	"strings"
)

var ErrNilServiceDesc = errors.New("nil service desc")
var ErrOperationNotFound = errors.New("operation not found")
var ErrOperationInvokerNotFound = errors.New("operation invoker not found")

type WSDLAssetSet struct {
	Main []byte
	XSD  map[string][]byte
}

type ServiceDesc struct {
	Name       string
	WSDL       *WSDLAssetSet
	Operations []OperationDesc
}

type OperationDesc struct {
	Operation   Operation
	NewRequest  func() any
	NewResponse func() any
	Invoke      func(ctx context.Context, impl any, req any) (any, error)
}

type operationInvoker interface {
	Invoke(ctx context.Context, operation string, req any) (any, error)
}

func (d *ServiceDesc) findOperationByWrapper(wrapper xml.Name) (OperationDesc, bool) {
	if d == nil {
		return OperationDesc{}, false
	}

	for _, op := range d.Operations {
		if op.Operation.RequestWrapper == wrapper {
			return op, true
		}
	}

	return OperationDesc{}, false
}

func (d *ServiceDesc) wsdlAsset() ([]byte, bool) {
	if d == nil || d.WSDL == nil || len(d.WSDL.Main) == 0 {
		return nil, false
	}

	return d.WSDL.Main, true
}

func (d *ServiceDesc) xsdAsset(name string) ([]byte, bool) {
	if d == nil || d.WSDL == nil || len(d.WSDL.XSD) == 0 {
		return nil, false
	}

	data, ok := d.WSDL.XSD[strings.TrimSpace(name)]
	if !ok || len(data) == 0 {
		return nil, false
	}

	return data, true
}

func (op OperationDesc) buildRequest() any {
	if op.NewRequest == nil {
		return nil
	}
	return op.NewRequest()
}

func (op OperationDesc) invoke(ctx context.Context, impl any, req any) (any, error) {
	if op.Invoke != nil {
		return op.Invoke(ctx, impl, req)
	}

	invoker, ok := impl.(operationInvoker)
	if !ok {
		return nil, ErrOperationInvokerNotFound
	}

	return invoker.Invoke(ctx, op.Operation.Name, req)
}
