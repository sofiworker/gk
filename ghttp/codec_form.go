package ghttp

import (
	"fmt"
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
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return err
	}
	switch target := v.(type) {
	case *url.Values:
		*target = values
		return nil
	case *map[string]string:
		out := make(map[string]string, len(values))
		for k, vs := range values {
			out[k] = vs[0]
		}
		*target = out
		return nil
	case *string:
		*target = string(data)
		return nil
	case *[]byte:
		*target = data
		return nil
	default:
		return fmt.Errorf("form codec: unsupported target %T", v)
	}
}
