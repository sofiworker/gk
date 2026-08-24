package app

import "encoding/xml"

// bindingQueryRequest /binding/query 的显式输入。
// explicit input for /binding/query.
type bindingQueryRequest struct {
	Name    string
	Count   int64
	Enabled bool
	Tags    []string
}

// bindingHeaderRequest /binding/header 的显式输入。
// explicit input for /binding/header.
type bindingHeaderRequest struct {
	TraceID string
	Count   int64
}

// bindingMixedRequest /binding/mixed 的显式输入。
// explicit input for /binding/mixed.
type bindingMixedRequest struct {
	ID      int64
	Query   string
	TraceID string
	Session string
	Name    string
}

// bindingNestedRequest /binding/nested 的显式输入。
// explicit input for /binding/nested.
type bindingNestedRequest struct {
	ProfileName string
	ProfileCity string
}

// codecJSONRequest /codec/json 的显式输入。
// explicit input for /codec/json.
type codecJSONRequest struct {
	Name string
}

// codecBodyRequest /codec/body/limited 的显式输入。
// explicit input for /codec/body/limited.
type codecBodyRequest struct {
	Data string
}

// codecNegotiationResponse 是 codec 协商端点的响应。
// codecNegotiationResponse is the response for the codec negotiation endpoint.
type codecNegotiationResponse struct {
	XMLName xml.Name `json:"-" xml:"response"`
	Name    string   `json:"name" xml:"name"`
}
