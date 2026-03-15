package gclient

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
)

type MultipartField struct {
	Name        string
	FileName    string
	ContentType string
	Reader      io.Reader
	FilePath    string
	Values      []string
}

func (r *Request) SetFile(fieldName, filePath string) *Request {
	return r.SetMultipartField(fieldName, filepath.Base(filePath), "", nil).setFilePath(filePath)
}

func (r *Request) SetFiles(files map[string]string) *Request {
	for fieldName, filePath := range files {
		r.SetFile(fieldName, filePath)
	}
	return r
}

func (r *Request) SetFileReader(fieldName, fileName string, reader io.Reader) *Request {
	return r.SetMultipartField(fieldName, fileName, "", reader)
}

func (r *Request) SetMultipartFormData(data map[string]string) *Request {
	for key, value := range data {
		r.multipartFields = append(r.multipartFields, &MultipartField{
			Name:   key,
			Values: []string{value},
		})
	}
	return r
}

func (r *Request) SetMultipartField(fieldName, fileName, contentType string, reader io.Reader) *Request {
	r.multipartFields = append(r.multipartFields, &MultipartField{
		Name:        fieldName,
		FileName:    fileName,
		ContentType: contentType,
		Reader:      reader,
	})
	return r
}

func (r *Request) SetMultipartFields(fields ...*MultipartField) *Request {
	for _, field := range fields {
		if field == nil {
			continue
		}
		cp := *field
		r.multipartFields = append(r.multipartFields, &cp)
	}
	return r
}

func (r *Request) SetMultipartBoundary(boundary string) *Request {
	r.multipartBoundary = boundary
	return r
}

func (r *Request) SetResponseSaveFileName(file string) *Request {
	r.responseSaveFileName = file
	return r
}

func (r *Request) SetResponseSaveToFile(save bool) *Request {
	r.isResponseSaveToFile = save
	return r
}

func (r *Request) setFilePath(filePath string) *Request {
	if len(r.multipartFields) == 0 {
		return r
	}
	r.multipartFields[len(r.multipartFields)-1].FilePath = filePath
	return r
}

func (b *httpRequestBuilder) prepareMultipartBody() (io.ReadCloser, string, error) {
	buf := b.client.bufferPool.Get(4096)
	writer := multipart.NewWriter(buf)
	if boundary := b.req.multipartBoundary; boundary != "" {
		if err := writer.SetBoundary(boundary); err != nil {
			b.client.bufferPool.Put(buf)
			return nil, "", err
		}
	}

	for _, field := range b.req.multipartFields {
		if field == nil {
			continue
		}
		if err := writeMultipartField(writer, field); err != nil {
			b.client.bufferPool.Put(buf)
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		b.client.bufferPool.Put(buf)
		return nil, "", err
	}

	b.req.bodyBytes = append([]byte(nil), buf.Bytes()...)
	b.client.bufferPool.Put(buf)
	return io.NopCloser(bytes.NewReader(b.req.bodyBytes)), writer.FormDataContentType(), nil
}

func writeMultipartField(writer *multipart.Writer, field *MultipartField) error {
	if len(field.Values) > 0 {
		for _, value := range field.Values {
			if err := writer.WriteField(field.Name, value); err != nil {
				return err
			}
		}
		return nil
	}

	reader, fileName, contentType, closeFn, err := resolveMultipartSource(field)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="`+field.Name+`"; filename="`+fileName+`"`)
	header.Set("Content-Type", contentType)

	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, reader)
	return err
}

func resolveMultipartSource(field *MultipartField) (io.Reader, string, string, func(), error) {
	if field.Reader != nil {
		fileName := field.FileName
		if fileName == "" {
			fileName = field.Name
		}
		return field.Reader, fileName, field.ContentType, nil, nil
	}

	if field.FilePath == "" {
		return nil, "", "", nil, http.ErrMissingFile
	}

	file, err := os.Open(field.FilePath)
	if err != nil {
		return nil, "", "", nil, err
	}
	fileName := field.FileName
	if fileName == "" {
		fileName = filepath.Base(field.FilePath)
	}
	return file, fileName, field.ContentType, func() { _ = file.Close() }, nil
}
