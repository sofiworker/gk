package layers

import (
	"testing"
)

type MockLayer struct {
	BaseLayer
	typ LayerType
}

func (m *MockLayer) LayerType() LayerType {
	return m.typ
}

func (m *MockLayer) String() string {
	return "mock"
}

type MockDecoder struct{}

func (m *MockDecoder) Decode(data []byte) (Layer, error) {
	return &MockLayer{
		BaseLayer: BaseLayer{Contents: data, PayloadData: data},
		typ:       LayerType(100),
	}, nil
}

func TestLayerRegistry(t *testing.T) {
	decoder := &MockDecoder{}
	// 注册自定义类型；register a custom type.
	customType := LayerType(100)
	RegisterLayerDecoder(customType, decoder)

	// 解码；decode.
	data := []byte("test")
	l, err := DecodeLayer(customType, data)
	if err != nil {
		t.Fatalf("DecodeLayer failed: %v", err)
	}

	if string(l.Payload()) != "test" {
		t.Errorf("payload mismatch")
	}

	// 解码未知类型；decode an unknown type.
	_, err = DecodeLayer(LayerType(999), data)
	if err == nil {
		t.Error("expected error for unknown layer")
	}
}

func TestBaseLayer(t *testing.T) {
	b := &BaseLayer{
		Contents:    []byte("header+payload"),
		PayloadData: []byte("payload"),
	}

	if b.Length() != 14 {
		t.Errorf("Length %d", b.Length())
	}
	if string(b.Payload()) != "payload" {
		t.Errorf("Payload mismatch")
	}
}
