package app

import (
	"encoding/xml"

	"github.com/sofiworker/gk/ghttp"
)

type bindingPathRequest struct {
	ghttp.Params
	ID int64 `path:"id"`
}
type bindingQueryRequest struct {
	ghttp.Params
	Name    string `query:"name"`
	Count   int64  `query:"count"`
	Enabled bool   `query:"enabled"`
}
type bindingHeaderRequest struct {
	ghttp.Params
	TraceID string `header:"X-Trace-ID"`
	Count   int64  `header:"X-Count"`
}
type bindingCookieRequest struct {
	ghttp.Params
	Session string `cookie:"session"`
}
type bindingMixedRequest struct {
	ghttp.Params
	ID      int64  `path:"id"`
	Query   string `query:"q"`
	TraceID string `header:"X-Trace-ID"`
	Session string `cookie:"session"`
	Body    struct {
		Name string `json:"name"`
	}
}
type bindingTimeRequest struct {
	ghttp.Params
	At string `query:"at"`
}
type bindingEnumRequest struct {
	ghttp.Params
	Status string `query:"status"`
}
type bindingNestedRequest struct {
	ghttp.Params
	ProfileName string `query:"profile_name"`
	ProfileCity string `query:"profile_city"`
}
type codecJSONRequest struct {
	Body struct {
		Name string `json:"name"`
	}
}
type codecXMLRequest struct {
	Body struct {
		Name string `xml:"name"`
	}
}
type codecFormRequest struct {
	Body struct {
		Name string `form:"name"`
		Note string `form:"note"`
	}
}
type codecMultipartRequest struct {
	Body struct {
		Note  string              `form:"note"`
		Files []*ghttp.FileHeader `form:"file"`
	}
}
type codecBodyRequest struct {
	Body struct {
		Data string `json:"data"`
	}
}

type codecNegotiationResponse struct {
	XMLName xml.Name `json:"-" xml:"response"`
	Name    string   `json:"name" xml:"name"`
}
