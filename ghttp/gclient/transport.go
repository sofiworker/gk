package gclient

import (
	"errors"
	"net/http"
)

func (c *Client) RoundTrip(req *http.Request) (*http.Response, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	return resp.ToHTTPResponse(), nil
}
