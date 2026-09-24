package v2

import "mime/multipart"

// MultipartStreamInput 返回逐 part 读取的 multipart reader，不将整个上传缓存在内存或临时文件中。
// MultipartStreamInput returns a part-by-part multipart reader without buffering the full upload in memory or temporary files.
// handler 负责读取并关闭每个 part；端点 WithBodyLimit 仍限制整个输入。
// The handler reads and closes each part; WithBodyLimit still bounds the entire input.
func MultipartStreamInput() Input[*multipart.Reader] {
	return CustomInput[*multipart.Reader](multipartStreamDecoder{})
}

type multipartStreamDecoder struct{}

func (multipartStreamDecoder) ContentType() string { return "multipart/form-data" }
func (multipartStreamDecoder) Decode(req *Request, dst **multipart.Reader) error {
	reader, err := req.MultipartReader()
	if err != nil {
		return err
	}
	*dst = reader
	return nil
}
