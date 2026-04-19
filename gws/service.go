package gws

import (
	"context"
	"encoding/xml"
	"errors"
	"strings"
)

// ErrNilServiceDesc indicates that a handler was created without a service
// description.
var ErrNilServiceDesc = errors.New("nil service desc")

// ErrOperationNotFound indicates that an inbound SOAP wrapper did not match any
// declared service operation.
var ErrOperationNotFound = errors.New("operation not found")

// ErrOperationInvokerNotFound indicates that an implementation object could not
// dispatch an operation dynamically.
var ErrOperationInvokerNotFound = errors.New("operation invoker not found")

// ErrMissingRequestFactory indicates that a service operation cannot decode an
// inbound request because no request factory was configured.
var ErrMissingRequestFactory = errors.New("missing request factory")

// WSDLAssetSet stores embedded WSDL and related XSD assets published by a
// service handler.
type WSDLAssetSet struct {
	Main []byte
	XSD  map[string][]byte
}

// ServiceDesc describes a SOAP service, its operations and optional published
// WSDL assets.
type ServiceDesc struct {
	Name       string
	WSDL       *WSDLAssetSet
	Operations []OperationDesc
}

// OperationDesc describes a SOAP operation together with factories and invoke
// wiring used by handlers and third-party integrations.
type OperationDesc struct {
	Operation   Operation
	NewRequest  func() any
	NewResponse func() any
	Invoke      func(ctx context.Context, impl any, req any) (any, error)
}

type operationInvoker interface {
	Invoke(ctx context.Context, operation string, req any) (any, error)
}

// FindOperationByWrapper resolves an operation by its request wrapper element.
func (d *ServiceDesc) FindOperationByWrapper(wrapper xml.Name) (OperationDesc, bool) {
	return d.findOperationByWrapper(wrapper)
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

// WSDLAsset returns the embedded primary WSDL document when present.
func (d *ServiceDesc) WSDLAsset() ([]byte, bool) {
	return d.wsdlAsset()
}

func (d *ServiceDesc) wsdlAsset() ([]byte, bool) {
	if d == nil || d.WSDL == nil || len(d.WSDL.Main) == 0 {
		return nil, false
	}

	return d.WSDL.Main, true
}

// XSDAsset returns an embedded XSD document by file name when present.
func (d *ServiceDesc) XSDAsset(name string) ([]byte, bool) {
	return d.xsdAsset(name)
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

// NewRequestValue constructs a new typed request value using the configured
// factory.
func (op OperationDesc) NewRequestValue() any {
	return op.buildRequest()
}

func (op OperationDesc) buildRequest() any {
	if op.NewRequest == nil {
		return nil
	}
	return op.NewRequest()
}

// NewResponseValue constructs a new typed response value using the configured
// factory.
func (op OperationDesc) NewResponseValue() any {
	if op.NewResponse == nil {
		return nil
	}
	return op.NewResponse()
}

// InvokeWith dispatches the operation against an implementation object.
func (op OperationDesc) InvokeWith(ctx context.Context, impl any, req any) (any, error) {
	return op.invoke(ctx, impl, req)
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
