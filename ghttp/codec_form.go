package ghttp

import (
	"io"
	"net/url"
)

// FormCodec handles application/x-www-form-urlencoded.
type FormCodec struct{}

func (c *FormCodec) ContentTypes() []string {
	return []string{"application/x-www-form-urlencoded"}
}

func (c *FormCodec) Marshal(w io.Writer, v interface{}) error {
	switch val := v.(type) {
	case url.Values:
		_, err := io.WriteString(w, val.Encode())
		return err
	case map[string]string:
		vals := url.Values{}
		for k, v := range val {
			vals.Set(k, v)
		}
		_, err := io.WriteString(w, vals.Encode())
		return err
	}
	return nil
}

func (c *FormCodec) Unmarshal(r io.Reader, v interface{}) error {
	return nil
}
