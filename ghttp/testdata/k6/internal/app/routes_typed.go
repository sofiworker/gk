package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/sofiworker/gk/ghttp"
)

// 本文件注册非 raw 的 typed 路由,与 routes_*.go 中的 rawAdapter 路由形成对照:
// typed 路由直接使用 ghttp 的类型化入口(GetParams/PostParamsBody/PostBody 等),
// 覆盖 params 绑定与各类请求体(JSON/XML/form/multipart/text)。
// This file registers non-raw typed routes, contrasting the rawAdapter routes in
// routes_*.go: typed routes use ghttp's typed entries (GetParams/PostParamsBody/
// PostBody etc.), covering param binding and every request-body kind
// (JSON/XML/form/multipart/text).

// ——— typed 请求/响应类型 ———

type typedParamsInput struct {
	ID     int64  `path:"id" validate:"required,min=1"`
	Trace  string `header:"X-Trace-ID"`
	Lang   string `query:"lang"`
	Weight int    `query:"weight"`
}

type typedParamsOutput struct {
	ID     int64  `json:"id"`
	Trace  string `json:"trace"`
	Lang   string `json:"lang"`
	Weight int    `json:"weight"`
}

type typedJSONBody struct {
	Name  string   `json:"name"`
	Tags  []string `json:"tags"`
	Count int      `json:"count"`
}

// Validate 让 typed JSON body 走自动校验:Count 必须为正。
// Validate makes the typed JSON body auto-validated: Count must be positive.
func (b typedJSONBody) Validate() error {
	if b.Count <= 0 {
		return errTypedCountPositive
	}
	return nil
}

type typedXMLBody struct {
	XMLName struct{} `xml:"payload"`
	Name    string   `xml:"name" json:"name"`
}

type typedFormBody struct {
	Name string  `form:"name" json:"name"`
	Note string  `form:"note" json:"note"`
	Size int     `form:"size" json:"size"`
	OK   bool    `form:"ok" json:"ok"`
	Rate float64 `form:"rate" json:"rate"`
}

type typedMixedParams struct {
	ID int64 `path:"id" validate:"required,min=1"`
}

// typedUploadBody 是 /typed/upload 的表单请求体:文件归请求体,经 FormBody[T]() 解码。
// typedUploadBody is the form body for /typed/upload: the file belongs to the
// request body, decoded via FormBody[T]().
type typedUploadBody struct {
	File ghttp.Upload `form:"file"`
}

// typedUploadMixedBody 是 /typed/upload-mixed 的表单请求体:文本字段与文件同处一个请求体。
// typedUploadMixedBody is the form body for /typed/upload-mixed: a text field and
// a file share one request body.
type typedUploadMixedBody struct {
	Note string       `form:"note"`
	File ghttp.Upload `form:"file"`
}

type typedUploadOutput struct {
	Note     string `json:"note"`
	Filename string `json:"filename"`
	Size     int    `json:"size"`
	Hash     string `json:"hash"`
}

type typedEchoOutput struct {
	Value string `json:"value"`
}

var errTypedCountPositive = fmt.Errorf("%w: count must be positive", ghttp.ErrValidation)

// registerTyped 注册全部非 raw 路由。
// registerTyped registers all non-raw routes.
func registerTyped(server *ghttp.Server) {
	mustTyped(ghttp.GetParams(server, "/typed/params/{id}", ghttp.JSON[typedParamsOutput](),
		func(_ context.Context, p typedParamsInput) (typedParamsOutput, error) {
			return typedParamsOutput{ID: p.ID, Trace: p.Trace, Lang: p.Lang, Weight: p.Weight}, nil
		}))

	mustTyped(ghttp.PostBody(server, "/typed/json", ghttp.JSONBody[typedJSONBody](), ghttp.JSON[typedJSONBody](),
		func(_ context.Context, b typedJSONBody) (typedJSONBody, error) {
			return b, nil
		}))

	mustTyped(ghttp.PostBody(server, "/typed/xml", ghttp.XMLBody[typedXMLBody](), ghttp.JSON[typedXMLBody](),
		func(_ context.Context, b typedXMLBody) (typedXMLBody, error) {
			return b, nil
		}))

	mustTyped(ghttp.PostBody(server, "/typed/form", ghttp.FormBody[typedFormBody](), ghttp.JSON[typedFormBody](),
		func(_ context.Context, b typedFormBody) (typedFormBody, error) {
			return b, nil
		}))

	mustTyped(ghttp.PostBody(server, "/typed/text", ghttp.TextBody[string](), ghttp.JSON[typedEchoOutput](),
		func(_ context.Context, body string) (typedEchoOutput, error) {
			return typedEchoOutput{Value: body}, nil
		}))

	mustTyped(ghttp.PostParamsBody(server, "/typed/mixed/{id}", ghttp.JSONBody[typedJSONBody](), ghttp.JSON[typedJSONBody](),
		func(_ context.Context, p typedMixedParams, b typedJSONBody) (typedJSONBody, error) {
			return b, nil
		}))

	mustTyped(ghttp.PostBody(server, "/typed/upload", ghttp.FormBody[typedUploadBody](), ghttp.JSON[typedUploadOutput](),
		func(_ context.Context, b typedUploadBody) (typedUploadOutput, error) {
			f, err := b.File.Open()
			if err != nil {
				return typedUploadOutput{}, err
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil {
				return typedUploadOutput{}, err
			}
			sum := sha256.Sum256(data)
			return typedUploadOutput{
				Filename: b.File.Filename,
				Size:     len(data),
				Hash:     hex.EncodeToString(sum[:]),
			}, nil
		}))

	mustTyped(ghttp.PostBody(server, "/typed/upload-mixed", ghttp.FormBody[typedUploadMixedBody](), ghttp.JSON[typedUploadOutput](),
		func(_ context.Context, b typedUploadMixedBody) (typedUploadOutput, error) {
			f, err := b.File.Open()
			if err != nil {
				return typedUploadOutput{}, err
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil {
				return typedUploadOutput{}, err
			}
			sum := sha256.Sum256(data)
			return typedUploadOutput{
				Note:     b.Note,
				Filename: b.File.Filename,
				Size:     len(data),
				Hash:     hex.EncodeToString(sum[:]),
			}, nil
		}))

	mustTyped(ghttp.GetNone(server, "/typed/none", ghttp.JSON[typedEchoOutput](),
		func(context.Context) (typedEchoOutput, error) {
			return typedEchoOutput{Value: "none"}, nil
		}))
}

// mustTyped 注册失败即 panic(注册期错误属于编程错误)。
// mustTyped panics on registration failure (registration errors are programmer errors).
func mustTyped(err error) {
	if err != nil {
		panic(err)
	}
}
