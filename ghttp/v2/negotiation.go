package v2

import (
	"errors"
	"mime"
	"strconv"
	"strings"
)

// ErrNotAcceptable 表示没有符合 Accept 的响应表示。
// ErrNotAcceptable reports that no representation satisfies Accept.
var ErrNotAcceptable error = notAcceptableError{}

type notAcceptableError struct{}

func (notAcceptableError) Error() string   { return "ghttp/v2: no acceptable representation" }
func (notAcceptableError) HTTPStatus() int { return 406 }

// WithNegotiation 按 Accept 选择同类型输出，未提供 Accept 时使用首项；同权重按声明顺序选择。
// WithNegotiation selects same-typed outputs by Accept, defaulting to the first for absent Accept and ties.
func WithNegotiation[O any](outputs ...Output[O]) Option {
	snapshot := append([]Output[O](nil), outputs...)
	return func(c *routeOptions) { c.negotiation = snapshot }
}

type mediaRange struct {
	kind        string
	params      map[string]string
	q           float64
	specificity int
}

func compileNegotiation[O any](config any) (func([]string) (func(*Response, O) error, error), error) {
	if config == nil {
		return nil, nil
	}
	outputs, ok := config.([]Output[O])
	if !ok || len(outputs) == 0 {
		return nil, errors.New("ghttp/v2: invalid negotiated output types")
	}
	types := make([]string, len(outputs))
	params := make([]map[string]string, len(outputs))
	encoders := make([]func(*Response, O) error, len(outputs))
	for i, out := range outputs {
		if !out.configured || out.encoder == nil || !out.hasBody || out.status != 0 && (out.status < 200 || out.status > 599) {
			return nil, errors.New("ghttp/v2: invalid negotiated output")
		}
		var err error
		types[i], params[i], err = mime.ParseMediaType(out.contentType)
		if err != nil || !strings.Contains(types[i], "/") || strings.Contains(types[i], "*") {
			return nil, errors.New("ghttp/v2: negotiated output requires a concrete media type")
		}
		encoders[i] = out.compileEncoder()
	}
	return func(headers []string) (func(*Response, O) error, error) {
		if len(headers) == 0 {
			return encoders[0], nil
		}
		ranges := parseAccept(strings.Join(headers, ","))
		best, bestQ := -1, float64(-1)
		for i, kind := range types {
			quality, specificity, paramCount := float64(0), -1, -1
			for _, r := range ranges {
				if r.kind != "*/*" && r.kind != kind && r.kind != strings.SplitN(kind, "/", 2)[0]+"/*" {
					continue
				}
				match := true
				for k, v := range r.params {
					if !strings.EqualFold(params[i][k], v) {
						match = false
						break
					}
				}
				if !match {
					continue
				}
				if r.specificity > specificity || r.specificity == specificity && len(r.params) > paramCount {
					quality, specificity, paramCount = r.q, r.specificity, len(r.params)
				}
			}
			if quality > 0 && quality > bestQ {
				best, bestQ = i, quality
			}
		}
		if best < 0 {
			return nil, ErrNotAcceptable
		}
		return encoders[best], nil
	}, nil
}

// 带引号的媒体参数可以包含逗号，不能直接 strings.Split。
// Quoted media parameters may contain commas and cannot use strings.Split.
func parseAccept(header string) []mediaRange {
	var result []mediaRange
	start, quoted, escaped := 0, false, false
	for i := 0; i <= len(header); i++ {
		if i < len(header) {
			c := header[i]
			if escaped {
				escaped = false
				continue
			}
			if quoted && c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				quoted = !quoted
				continue
			}
			if c != ',' || quoted {
				continue
			}
		}
		kind, params, err := mime.ParseMediaType(strings.TrimSpace(header[start:i]))
		start = i + 1
		if err != nil {
			continue
		}
		q := float64(1)
		if raw, ok := params["q"]; ok {
			q, err = strconv.ParseFloat(raw, 64)
			delete(params, "q")
			if err != nil || !(q >= 0 && q <= 1) {
				continue
			}
		}
		spec := 2
		if kind == "*/*" {
			spec = 0
		} else if strings.HasSuffix(kind, "/*") {
			spec = 1
		}
		result = append(result, mediaRange{kind: kind, params: params, q: q, specificity: spec})
	}
	return result
}
