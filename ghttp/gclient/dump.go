package gclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

func (c *Client) SetDebug(debug bool) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.debug = debug
	if c.config == nil {
		c.config = DefaultConfig()
	}
	if c.config.DumpConfig == nil {
		c.config.DumpConfig = &DumpConfig{}
	}
	c.config.DumpConfig.DumpRequest = debug
	c.config.DumpConfig.DumpResponse = debug
	return c
}

func (c *Client) SetDumpRequest(dump bool) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config == nil {
		c.config = DefaultConfig()
	}
	if c.config.DumpConfig == nil {
		c.config.DumpConfig = &DumpConfig{}
	}
	c.config.DumpConfig.DumpRequest = dump
	return c
}

func (c *Client) SetDumpResponse(dump bool) *Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config == nil {
		c.config = DefaultConfig()
	}
	if c.config.DumpConfig == nil {
		c.config.DumpConfig = &DumpConfig{}
	}
	c.config.DumpConfig.DumpResponse = dump
	return c
}

func (r *Request) Dump() (string, error) {
	httpReq, err := r.BuildHTTPRequest()
	if err != nil {
		return "", err
	}
	return dumpHTTPRequest(httpReq)
}

func (r *Request) MustDump() string {
	dump, err := r.Dump()
	if err != nil {
		panic(err)
	}
	return dump
}

func (r *Response) Dump() string {
	return dumpHTTPResponse(r)
}

func dumpHTTPRequest(req *http.Request) (string, error) {
	if req == nil {
		return "", nil
	}

	var body []byte
	if req.Body != nil {
		data, err := io.ReadAll(req.Body)
		if err != nil {
			return "", err
		}
		body = data
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	var builder strings.Builder
	builder.WriteString(req.Method)
	builder.WriteString(" ")
	builder.WriteString(req.URL.String())
	builder.WriteString(" ")
	builder.WriteString(req.Proto)
	builder.WriteString("\n")
	builder.WriteString(formatHeader(req.Header))
	if len(body) > 0 {
		builder.WriteString("\n")
		builder.Write(body)
	}

	return builder.String(), nil
}

func dumpHTTPResponse(resp *Response) string {
	if resp == nil {
		return ""
	}

	var builder strings.Builder
	status := strings.TrimSpace(resp.Status)
	if status == "" && resp.StatusCode > 0 {
		status = fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	if resp.Proto != "" {
		builder.WriteString(resp.Proto)
	} else {
		builder.WriteString("HTTP/1.1")
	}
	if status != "" {
		builder.WriteString(" ")
		builder.WriteString(status)
	}
	builder.WriteString("\n")
	builder.WriteString(formatHeader(resp.Header))
	if len(resp.Body) > 0 {
		builder.WriteString("\n")
		builder.Write(resp.Body)
	}

	return builder.String()
}

func formatHeader(header http.Header) string {
	if len(header) == 0 {
		return ""
	}

	keys := make([]string, 0, len(header))
	for key := range header {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		for _, value := range header.Values(key) {
			builder.WriteString(key)
			builder.WriteString(": ")
			builder.WriteString(value)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}
