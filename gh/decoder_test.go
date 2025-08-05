package ghttp

import (
	"strings"
	"testing"
)

func TestJsonDecoder_Decode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]interface{}
		wantErr bool
	}{
		{
			name:  "正常JSON解析",
			input: `{"name": "test", "age": 18}`,
			want:  map[string]interface{}{"name": "test", "age": float64(18)},
		},
		{
			name:    "异常JSON格式",
			input:   `{"name": "test", "age": }`,
			wantErr: true,
		},
	}

	decoder := NewJsonDecoder()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]interface{}
			err := decoder.Decode(strings.NewReader(tt.input), &got)
			if (err != nil) != tt.wantErr {
				t.Errorf("Decode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !compareMap(got, tt.want) {
				t.Errorf("Decode() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestXmlDecoder_Decode(t *testing.T) {
	type testStruct struct {
		Name string `xml:"name"`
		Age  int    `xml:"age"`
	}

	tests := []struct {
		name    string
		input   string
		want    testStruct
		wantErr bool
	}{
		{
			name:  "正常XML解析",
			input: `<root><name>test</name><age>18</age></root>`,
			want:  testStruct{Name: "test", Age: 18},
		},
		{
			name:    "异常XML格式",
			input:   `<root><name>test</name><age>invalid</age></root>`,
			wantErr: true,
		},
	}

	decoder := NewXmlDecoder()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got testStruct
			err := decoder.Decode(strings.NewReader(tt.input), &got)
			if (err != nil) != tt.wantErr {
				t.Errorf("Decode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Decode() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestYamlDecoder_Decode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]interface{}
		wantErr bool
	}{
		{
			name: "正常YAML解析",
			input: `
name: test
age: 18`,
			want: map[string]interface{}{"name": "test", "age": 18},
		},
		{
			name: "异常YAML格式",
			input: `
name: test
  age: 18`,
			wantErr: true,
		},
	}

	decoder := NewYamlDecoder()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]interface{}
			err := decoder.Decode(strings.NewReader(tt.input), &got)
			if (err != nil) != tt.wantErr {
				t.Errorf("Decode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !compareMap(got, tt.want) {
				t.Errorf("Decode() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func compareMap(m1, m2 map[string]interface{}) bool {
	if len(m1) != len(m2) {
		return false
	}
	for k, v1 := range m1 {
		if v2, ok := m2[k]; !ok || v1 != v2 {
			return false
		}
	}
	return true
}
