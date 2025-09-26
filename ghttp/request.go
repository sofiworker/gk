package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/valyala/fasthttp"
)

type Request struct {
	Client *Client

	url    string
	method string

	Headers     http.Header
	Cookies     []*http.Cookie
	QueryParams url.Values
	FormData    url.Values

	time struct {
		startRequest *time.Time
		endRequest   *time.Time

		startResponse *time.Time
		endResponse   *time.Time
	}

	Redirects struct {
		MaxTime int
	}

	Callback struct {
	}
}

func NewRequest() *Request {
	return &Request{
		Client: NewClient(),
	}
}

func (r *Request) GetClient() *Client {
	return r.Client
}

func (r *Request) SetHeader(key, value string) *Request {
	if r.Headers == nil {
		r.Headers = make(http.Header)
	}
	r.Headers.Set(key, value)
	return r
}

func (r *Request) SetHeaders(hdrs map[string]string) *Request {
	for k, v := range hdrs {
		r.SetHeader(k, v)
	}
	return r
}

func (r *Request) SetUrl(url string) *Request {
	r.url = url
	return r
}

func (r *Request) SetMethod(method string) *Request {
	r.method = method
	return r
}

func (r *Request) Get() (*Response, error) {
	return r.SetMethod(http.MethodGet).Done()
}

func (r *Request) HEAD() (*Response, error) {
	return r.SetMethod(http.MethodHead).Done()
}

func (r *Request) POST() (*Response, error) {
	return r.SetMethod(http.MethodPost).Done()
}

func (r *Request) PUT() (*Response, error) {
	return r.SetMethod(http.MethodPut).Done()
}

func (r *Request) PATCH() (*Response, error) {
	return r.SetMethod(http.MethodPatch).Done()
}

func (r *Request) DELETE() (*Response, error) {
	return r.SetMethod(http.MethodDelete).Done()
}

func (r *Request) CONNECT() (*Response, error) {
	return r.SetMethod(http.MethodConnect).Done()
}

func (r *Request) OPTIONS() (*Response, error) {
	return r.SetMethod(http.MethodOptions).Done()
}

func (r *Request) TRACE() (*Response, error) {
	return r.SetMethod(http.MethodTrace).Done()
}

func (r *Request) DownloadCallback(filename string, data <-chan []byte) {

}

func (r *Request) SetProgressCallback(callback func(current, total int64)) *Request {
	r.progressCallback = callback
	return r
}

// handleRequestBody 处理普通请求体
func (r *Request) handleRequestBody() error {
	// 检查是否是表单数据
	contentType := string(r.fr.Header.Peek("Content-Type"))
	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		// 处理表单数据
		if formData, ok := r.requestBody.(map[string]interface{}); ok {
			var formValues url.Values = make(url.Values)
			for key, value := range formData {
				formValues.Set(key, fmt.Sprintf("%v", value))
			}
			r.fr.SetBodyString(formValues.Encode())
		}
	} else {
		// 默认处理为JSON
		bs, err := json.Marshal(r.requestBody)
		if err != nil {
			return err
		}
		r.fr.SetBody(bs)
		if contentType == "" {
			r.fr.Header.Set("Content-Type", "application/json")
		}
	}
	return nil
}

// handleRedirects 处理重定向
func (r *Request) handleRedirects(resp *Response) (*Response, error) {

	// 检查状态码是否为重定向
	statusCode := resp.fResp.StatusCode()
	if statusCode < 300 || statusCode >= 400 {
		return nil, nil
	}

	// 检查是否有Location头
	location := resp.fResp.Header.Peek("Location")
	if len(location) == 0 {
		return nil, nil
	}

	// TODO: 实现重定向逻辑
	// 这里应该创建新的请求并执行重定向
	// 为简化示例，暂时返回nil表示不处理重定向

	return nil, nil
}

func (r *Request) UploadFileByReaderWithSizeAndFieldName(reader io.Reader, size int, fieldName string) *Request {
	r.fileReader, r.fileReaderSize = reader, size
	if fieldName != "" {
		r.fr.Header.Set("X-File-Field-Name", fieldName)
	}
	return r
}

func (r *Request) SetFormData(data map[string]string) *Request {
	if r.requestBody == nil {
		r.requestBody = make(map[string]interface{})
	}

	if bodyMap, ok := r.requestBody.(map[string]interface{}); ok {
		for k, v := range data {
			bodyMap[k] = v
		}
	}
	return r
}

func (r *Request) SetFormDataFromStruct(data interface{}) *Request {
	if r.requestBody == nil {
		r.requestBody = make(map[string]interface{})
	}

	if bodyMap, ok := r.requestBody.(map[string]interface{}); ok {
		v := reflect.ValueOf(data)
		t := reflect.TypeOf(data)

		for v.Kind() == reflect.Ptr {
			v = v.Elem()
			t = t.Elem()
		}

		if v.Kind() == reflect.Struct {
			for i := 0; i < v.NumField(); i++ {
				field := t.Field(i)
				fieldValue := v.Field(i)
				tag := field.Tag.Get("form")
				if tag == "" {
					tag = field.Name
				}
				bodyMap[tag] = fieldValue.Interface()
			}
		}
	}
	return r
}

func (r *Request) SetMultipartFormData(data map[string]string) *Request {
	r.fr.Header.Set("Content-Type", "multipart/form-data")
	r.requestBody = data
	return r
}
func (r *Request) handleFileUpload() error {
	// 检查是否启用 Pipe 上传
	if r.isLargeFileUpload {
		return r.handlePipeFileUpload()
	}

	// 对于大文件，我们使用流式上传而不是加载到内存
	if r.isFileLarge() {
		return r.handleLargeFileUpload()
	}

	// 对于小文件，保持原有的处理方式
	return r.handleSmallFileUpload()
}

// isFileLarge 判断文件是否为大文件（超过100MB）
func (r *Request) isFileLarge() bool {
	// 如果指定了文件大小且超过100MB，则认为是大文件
	if r.fileReaderSize > 0 && r.fileReaderSize > 100*1024*1024 {
		return true
	}

	// 如果有文件路径，检查文件大小
	if r.file != "" {
		if fileInfo, err := os.Stat(r.file); err == nil {
			return fileInfo.Size() > 100*1024*1024 // 100MB
		}
	}

	return false
}

// handleSmallFileUpload 处理小文件上传，保持原有的multipart方式
func (r *Request) handleSmallFileUpload() error {
	// 创建multipart表单
	var bodyBuffer bytes.Buffer
	writer := multipart.NewWriter(&bodyBuffer)

	// 添加表单字段
	if r.requestBody != nil {
		if formData, ok := r.requestBody.(map[string]interface{}); ok {
			for key, value := range formData {
				writer.WriteField(key, fmt.Sprintf("%v", value))
			}
		}
	}

	// 添加文件
	fieldName := r.fr.Header.Peek("X-File-Field-Name")
	if len(fieldName) == 0 {
		fieldName = []byte("file")
	}

	if r.file != "" {
		// 从文件路径上传
		file, err := os.Open(r.file)
		if err != nil {
			return err
		}
		defer file.Close()

		part, err := writer.CreateFormFile(string(fieldName), filepath.Base(r.file))
		if err != nil {
			return err
		}

		_, err = io.Copy(part, file)
		if err != nil {
			return err
		}
	} else if r.fileReader != nil {
		// 从reader上传
		part, err := writer.CreateFormFile(string(fieldName), "upload")
		if err != nil {
			return err
		}

		_, err = io.Copy(part, r.fileReader)
		if err != nil {
			return err
		}
	}

	err := writer.Close()
	if err != nil {
		return err
	}

	// 设置请求体和Content-Type
	r.fr.SetBody(bodyBuffer.Bytes())
	r.fr.Header.Set("Content-Type", writer.FormDataContentType())

	// 清理临时header
	r.fr.Header.Del("X-File-Field-Name")

	return nil
}

// UploadFileWithProgress 上传文件并提供进度回调
func (r *Request) UploadFileWithProgress(path string, callback func(current, total int64)) *Request {
	r.file = path
	r.progressCallback = callback
	return r
}

// UploadFileByReaderWithProgress 通过reader上传文件并提供进度回调
func (r *Request) UploadFileByReaderWithProgress(reader io.Reader, size int, callback func(current, total int64)) *Request {
	r.fileReader, r.fileReaderSize = reader, size
	r.progressCallback = callback
	return r
}

type ProgressReader struct {
	reader   io.Reader
	total    int64
	current  int64
	callback func(current, total int64)
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.current += int64(n)
	if pr.callback != nil {
		pr.callback(pr.current, pr.total)
	}
	return n, err
}

// 修改 handleLargeFileUpload 以支持进度回调
func (r *Request) handleLargeFileUpload() error {
	fieldName := r.fr.Header.Peek("X-File-Field-Name")
	if len(fieldName) == 0 {
		fieldName = []byte("file")
	}

	r.isLargeFileUpload = true

	// 如果有表单数据，需要特殊处理
	if r.requestBody != nil {
		if formData, ok := r.requestBody.(map[string]interface{}); ok {
			for key, value := range formData {
				r.fr.Header.Set("X-Form-"+key, fmt.Sprintf("%v", value))
			}
		}
	}

	if r.file != "" {
		// 从文件路径上传大文件
		file, err := os.Open(r.file)
		if err != nil {
			return err
		}

		r.fileHandle = file

		var bodyReader io.Reader = file

		// 如果有进度回调，包装Reader
		if r.progressCallback != nil {
			var fileSize int64
			if fileInfo, err := file.Stat(); err == nil {
				fileSize = fileInfo.Size()
			}
			bodyReader = &ProgressReader{
				reader:   file,
				total:    fileSize,
				callback: r.progressCallback,
			}
		}

		// 设置流式body
		r.fr.SetBodyStream(bodyReader, -1)

		// 设置相关头部
		r.fr.Header.Set("Content-Type", "application/octet-stream")
		r.fr.Header.Set("X-File-Name", filepath.Base(r.file))
		r.fr.Header.Set("X-File-Field", string(fieldName))
	} else if r.fileReader != nil {
		var bodyReader io.Reader = r.fileReader

		// 如果有进度回调，包装Reader
		if r.progressCallback != nil {
			bodyReader = &ProgressReader{
				reader:   r.fileReader,
				total:    int64(r.fileReaderSize),
				callback: r.progressCallback,
			}
		}

		r.fr.SetBodyStream(bodyReader, r.fileReaderSize)
		r.fr.Header.Set("Content-Type", "application/octet-stream")
		r.fr.Header.Set("X-File-Name", "upload")
		r.fr.Header.Set("X-File-Field", string(fieldName))
	}

	r.fr.Header.Del("X-File-Field-Name")

	io.Pipe()
	return nil
}

// PipeFileUpload 使用 io.Pipe 进行文件上传，适用于大文件流式传输
func (r *Request) PipeFileUpload() *Request {
	r.isLargeFileUpload = true
	return r
}

// handlePipeFileUpload 使用 io.Pipe 处理文件上传
func (r *Request) handlePipeFileUpload() error {
	// 创建管道
	pr, pw := io.Pipe()

	// 设置请求体为管道读取端
	r.fr.SetBodyStream(pr, -1)

	// 在单独的 goroutine 中写入数据到管道
	go func() {
		defer pw.Close()

		fieldName := r.fr.Header.Peek("X-File-Field-Name")
		if len(fieldName) == 0 {
			fieldName = []byte("file")
		}

		// 创建 multipart writer
		writer := multipart.NewWriter(pw)

		// 设置 Content-Type
		r.fr.Header.Set("Content-Type", writer.FormDataContentType())

		// 添加表单字段
		if r.requestBody != nil {
			if formData, ok := r.requestBody.(map[string]interface{}); ok {
				for key, value := range formData {
					writer.WriteField(key, fmt.Sprintf("%v", value))
				}
			}
		}

		// 添加文件部分
		if r.file != "" {
			// 从文件路径上传
			file, err := os.Open(r.file)
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			defer file.Close()

			part, err := writer.CreateFormFile(string(fieldName), filepath.Base(r.file))
			if err != nil {
				pw.CloseWithError(err)
				return
			}

			// 如果有进度回调，使用 ProgressReader
			var reader io.Reader = file
			if r.progressCallback != nil {
				if fileInfo, err := file.Stat(); err == nil {
					reader = &ProgressReader{
						reader:   file,
						total:    fileInfo.Size(),
						callback: r.progressCallback,
					}
				}
			}

			// 复制文件内容到 multipart
			_, err = io.Copy(part, reader)
			if err != nil {
				pw.CloseWithError(err)
				return
			}
		} else if r.fileReader != nil {
			// 从 reader 上传
			part, err := writer.CreateFormFile(string(fieldName), "upload")
			if err != nil {
				pw.CloseWithError(err)
				return
			}

			// 如果有进度回调，使用 ProgressReader
			var reader io.Reader = r.fileReader
			if r.progressCallback != nil {
				reader = &ProgressReader{
					reader:   r.fileReader,
					total:    int64(r.fileReaderSize),
					callback: r.progressCallback,
				}
			}

			// 复制内容到 multipart
			_, err = io.Copy(part, reader)
			if err != nil {
				pw.CloseWithError(err)
				return
			}
		}

		// 关闭 multipart writer
		err := writer.Close()
		if err != nil {
			pw.CloseWithError(err)
			return
		}
	}()

	// 清理临时header
	r.fr.Header.Del("X-File-Field-Name")

	return nil
}

// UploadLargeFile 使用 io.Pipe 上传大文件，支持进度监控
func (r *Request) UploadLargeFile(path string, fieldName string, progressCallback func(current, total int64)) *Request {
	r.file = path
	r.isLargeFileUpload = true
	r.progressCallback = progressCallback

	if fieldName != "" {
		r.fr.Header.Set("X-File-Field-Name", fieldName)
	}

	return r
}

// UploadLargeFileByReader 使用 io.Pipe 通过 reader 上传大文件，支持进度监控
func (r *Request) UploadLargeFileByReader(reader io.Reader, size int, fieldName string, progressCallback func(current, total int64)) *Request {
	r.fileReader = reader
	r.fileReaderSize = size
	r.isLargeFileUpload = true
	r.progressCallback = progressCallback

	if fieldName != "" {
		r.fr.Header.Set("X-File-Field-Name", fieldName)
	}

	return r
}

func (r *Request) SetIfModifiedSince(time time.Time) *Request {
	r.fr.Header.Set("If-Modified-Since", time.Format(http.TimeFormat))
	return r
}

func (r *Request) SetIfNoneMatch(etag string) *Request {
	r.fr.Header.Set("If-None-Match", etag)
	return r
}

func (r *Request) SetMaxRedirects(max int) *Request {
	// 在请求级别设置重定向配置
	return r
}

func (r *Request) SetFollowRedirects(follow bool) *Request {
	// 在请求级别设置重定向配置
	return r
}

func (r *Request) AddRedirectHandler(handler func(*Response) bool) *Request {
	// 在请求级别添加重定向处理函数
	return r
}

func (r *Request) Done() (*Response, error) {
	if r.method == "" {
		return nil, ErrNotFoundMethod
	}

	// 获取重试配置
	retryConfig := r.retryConfig
	if retryConfig == nil {
		//retryConfig = &r.Client.retryConfig
	}

	var lastErr error
	var lastResp *Response

	startTime := time.Now()

	for attempt := 0; attempt <= retryConfig.MaxRetries; attempt++ {
		// 检查是否超过最大重试时间
		if retryConfig.MaxRetryTime > 0 {
			if time.Since(startTime) > retryConfig.MaxRetryTime {
				break
			}
		}

		// 执行请求
		resp, err := r.executeRequest()
		lastErr = err
		lastResp = resp

		// 检查是否需要重试
		shouldRetry := false
		if len(retryConfig.RetryConditions) > 0 {
			for _, condition := range retryConfig.RetryConditions {
				if condition(resp, err) {
					shouldRetry = true
					break
				}
			}
		} else {
			// 使用默认重试条件
			shouldRetry = DefaultRetryCondition(resp, err)
		}

		if !shouldRetry || attempt >= retryConfig.MaxRetries {
			return resp, err
		}

		// 计算延迟时间
		delay := time.Duration(0)
		if retryConfig.Backoff != nil {
			delay = retryConfig.Backoff(attempt)
		} else {
			// 默认退避策略
			delay = time.Millisecond * 100 * time.Duration(1<<uint(attempt))
		}

		// 检查延迟后是否超过最大重试时间
		if retryConfig.MaxRetryTime > 0 {
			if time.Since(startTime)+delay > retryConfig.MaxRetryTime {
				break
			}
		}

		time.Sleep(delay)
	}

	return lastResp, lastErr
}

func (r *Request) executeRequest() (*Response, error) {
	if r.returnData != nil {
		if reflect.TypeOf(r.returnData).Kind() != reflect.Ptr {
			return nil, ErrDataFormat
		}
	}

	addr, err := ConstructURL(r.Client.BaseUrl, r.url)
	if err != nil {
		return nil, err
	}
	r.fr.SetRequestURI(addr)

	// 处理文件上传
	if r.file != "" || r.fileReader != nil {
		err = r.handleFileUpload()
		if err != nil {
			return nil, err
		}
	} else if r.requestBody != nil {
		// 处理普通请求体
		err = r.handleRequestBody()
		if err != nil {
			return nil, err
		}
	}

	resp := new(Response)
	response := fasthttp.AcquireResponse()
	resp.fResp = response
	// 确保在函数结束时释放资源
	defer func() {
		if r.fr != nil {
			fasthttp.ReleaseRequest(r.fr)
		}
		if response != nil {
			fasthttp.ReleaseResponse(resp.fResp)
		}
		// 关闭大文件句柄
		if r.fileHandle != nil {
			r.fileHandle.Close()
		}
	}()

	if r.tracer != nil {
		_, end := r.Client.tracer.StartSpan(context.TODO())
		defer end()
	}
	startTime := time.Now().UnixMicro()
	r.startRequest = startTime

	if r.resolver != nil {
		r.Client.fastClient.Dial = (&fasthttp.TCPDialer{
			Resolver: r.resolver,
		}).Dial
	}

	err = r.Client.fastClient.Do(r.fr, resp.fResp)
	if err != nil {
		return nil, err
	}
	endTime := time.Now().UnixMicro()
	costTime := endTime - startTime
	r.endRequest = endTime
	r.costRequest = costTime

	resp.RemoteAddr = resp.fResp.RemoteAddr().String()
	resp.BodyRaw = resp.fResp.Body()

	// 处理重定向
	redirectResp, err := r.handleRedirects(resp)
	if err != nil {
		return nil, err
	}
	if redirectResp != nil {
		resp = redirectResp
	}

	// 处理统一响应体
	if r.shouldProcessUnifiedResponse() {
		// 设置解码器
		mediaType, _, err := mime.ParseMediaType(string(resp.fResp.Header.ContentType()))
		if err != nil {
			// 如果无法解析Content-Type，使用默认解码器
			//resp.decoder = r.Client.defaultDecoder
		} else {
			if decoder, ok := decoders.Exist(mediaType); ok {
				resp.decoder = decoder
			} else {
				// 使用默认解码器
				//resp.decoder = r.Client.defaultDecoder
			}
		}

		if r.returnData != nil {
			// 使用统一响应体解码
			err = resp.UnifiedResponseDecode(nil, r.returnData)
			if err != nil {
				return nil, err
			}
		}
	} else {
		// 原有处理逻辑
		mediaType, _, err := mime.ParseMediaType(string(resp.fResp.Header.ContentType()))
		if err != nil {
			return nil, err
		}
		if decoder, ok := decoders.Exist(mediaType); ok {
			resp.decoder = decoder
		} else {
			// 使用默认解码器
			//resp.decoder = r.Client.defaultDecoder
		}

		if r.returnData != nil {
			if resp.decoder != nil {
				buffer := bytes.NewBuffer(resp.BodyRaw)
				err = resp.decoder.Decode(buffer, r.returnData)
				if err != nil {
					return nil, err
				}
			} else {
				err = json.Unmarshal(resp.fResp.Body(), r.returnData)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	return resp, nil
}
