package v3

import (
	"fmt"
	"reflect"
	"strings"
)

// ParameterSource 指定 OpenAPI 参数位置，不启用运行时解析或校验。
// ParameterSource specifies an OpenAPI parameter location without enabling runtime parsing or validation.
type ParameterSource string

const (
	// ParameterPath 声明路径参数。
	// ParameterPath declares a path parameter.
	ParameterPath ParameterSource = "path"
	// ParameterQuery 声明查询参数。
	// ParameterQuery declares a query parameter.
	ParameterQuery ParameterSource = "query"
	// ParameterHeader 声明请求头参数。
	// ParameterHeader declares a header parameter.
	ParameterHeader ParameterSource = "header"
	// ParameterCookie 声明 Cookie 参数。
	// ParameterCookie declares a cookie parameter.
	ParameterCookie ParameterSource = "cookie"
)

type parameterDescription struct {
	source   ParameterSource
	name     string
	required bool
	typ      reflect.Type
}

// WithParameter 为按需读取的参数声明类型与必需性；handler 仍负责读取、转换及校验。
// WithParameter documents the type and presence requirement of an on-demand parameter; the handler owns reading, conversion and validation.
// 同来源同名的后续声明覆盖前者；path 参数始终必需。
// Later declarations override the same source and name; path parameters are always required.
func WithParameter[T any](source ParameterSource, name string, required bool) Option {
	return func(c *routeOptions) {
		c.parameters = append(c.parameters, parameterDescription{source: source, name: name, required: required || source == ParameterPath, typ: reflect.TypeFor[T]()})
	}
}

func compileParameters(path string, descriptions []parameterDescription) ([]parameterDescription, error) {
	var result []parameterDescription
	for _, param := range descriptions {
		if param.name == "" || strings.ContainsAny(param.name, "\r\n") {
			return nil, fmt.Errorf("ghttp/v3: invalid parameter name %q", param.name)
		}
		switch param.source {
		case ParameterPath:
			found := false
			for _, name := range pathVariableNames(path) {
				found = found || name == param.name
			}
			if !found {
				return nil, fmt.Errorf("ghttp/v3: path parameter %q is not in route", param.name)
			}
		case ParameterQuery, ParameterHeader, ParameterCookie:
		default:
			return nil, fmt.Errorf("ghttp/v3: invalid parameter source %q", param.source)
		}
		replaced := false
		for i, previous := range result {
			if previous.source == param.source && (previous.name == param.name || param.source == ParameterHeader && strings.EqualFold(previous.name, param.name)) {
				result[i] = param
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, param)
		}
	}
	return result, nil
}
