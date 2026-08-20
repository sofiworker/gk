package app

import (
	"encoding/xml"

	"github.com/sofiworker/gk/ghttp"
)

// bindingPathRequest /binding/path/{id} 的显式输入。
// explicit input for /binding/path/{id}.
type bindingPathRequest struct {
	ID int64
}

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

// bindingCookieRequest /binding/cookie 的显式输入。
// explicit input for /binding/cookie.
type bindingCookieRequest struct {
	Session string
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

// bindingTimeRequest /binding/time 的显式输入。
// explicit input for /binding/time.
type bindingTimeRequest struct {
	At string
}

// bindingEnumRequest /binding/enum 的显式输入。
// explicit input for /binding/enum.
type bindingEnumRequest struct {
	Status string
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

// codecXMLRequest /codec/xml 的显式输入。
// explicit input for /codec/xml.
type codecXMLRequest struct {
	Name string
}

// codecFormRequest /codec/form 的显式输入。
// explicit input for /codec/form.
type codecFormRequest struct {
	Name string
	Note string
}

// codecMultipartRequest /codec/multipart 的显式输入。
// explicit input for /codec/multipart.
type codecMultipartRequest struct {
	Note  string
	Files []*ghttp.FileHeader
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
