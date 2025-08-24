package ghttp

import "fmt"

// 定义更丰富的错误类型
type HTTPError struct {
	StatusCode int
	Message    string
	Response   *Response
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

// 在请求处理中使用更精确的错误类型
// func (r *Request) Done() (*Response, error) {
// 	// ... 现有代码 ...

// 	err = r.client.fastClient.Do(r.fr, resp.fResp)
// 	if err != nil {
// 		return nil, err
// 	}

// 	// 检查HTTP状态码
// 	if resp.fResp.StatusCode() >= 400 {
// 		return nil, &HTTPError{
// 			StatusCode: resp.fResp.StatusCode(),
// 			Message:    string(resp.fResp.Body()),
// 			Response:   resp,
// 		}
// 	}

// 	// ... 现有代码 ...
// }
