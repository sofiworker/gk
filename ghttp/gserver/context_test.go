package gserver

import (
	"testing"

	"github.com/sofiworker/gk/gcodec"
	"github.com/valyala/fasthttp"
)

type TestStruct struct {
	Name string `json:"name" xml:"name"`
	Age  int    `json:"age" xml:"age"`
}

func TestContextJSON(t *testing.T) {
	// Create a test context
	ctx := &Context{
		fastCtx: &fasthttp.RequestCtx{},
		codec:   newCodecFactory(),
	}

	// Test JSON binding
	testData := `{"name":"test","age":25}`
	ctx.fastCtx.Request.SetBodyString(testData)

	var result TestStruct
	err := ctx.BindJSON(&result)
	if err != nil {
		t.Errorf("BindJSON failed: %v", err)
	}

	if result.Name != "test" || result.Age != 25 {
		t.Errorf("BindJSON result incorrect: got %+v", result)
	}
}

func TestContextXML(t *testing.T) {
	// Create a test context
	ctx := &Context{
		fastCtx: &fasthttp.RequestCtx{},
		codec:   newCodecFactory(),
	}

	// Test XML binding
	testData := `<TestStruct><name>test</name><age>25</age></TestStruct>`
	ctx.fastCtx.Request.SetBodyString(testData)

	var result TestStruct
	err := ctx.BindXML(&result)
	if err != nil {
		t.Errorf("BindXML failed: %v", err)
	}

	if result.Name != "test" || result.Age != 25 {
		t.Errorf("BindXML result incorrect: got %+v", result)
	}
}

func TestCodecFactory(t *testing.T) {
	cf := newCodecFactory()

	// Test JSON codec
	jsonCodec := cf.Get("application/json")
	if jsonCodec == nil {
		t.Error("JSON codec not found")
	}

	// Test XML codec
	xmlCodec := cf.Get("application/xml")
	if xmlCodec == nil {
		t.Error("XML codec not found")
	}

	// Test YAML codec
	yamlCodec := cf.Get("application/yaml")
	if yamlCodec == nil {
		t.Error("YAML codec not found")
	}

	// Test registering a new codec
	err := cf.Register("test/codec", gcodec.NewJSONCodec())
	if err != nil {
		t.Errorf("Failed to register codec: %v", err)
	}

	// Test duplicate registration
	err = cf.Register("test/codec", gcodec.NewJSONCodec())
	if err != ErrAlreadyRegistered {
		t.Error("Expected ErrAlreadyRegistered for duplicate codec")
	}
}
