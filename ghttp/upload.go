package ghttp

import (
	"fmt"
	"io"
	"mime/multipart"
	"os"
)

// Upload 承接一个 multipart 上传文件。它属于请求体:在 form 请求体结构体(经 FormBody[T]
// 解码)里放 Upload 字段即绑定同名文件,放 []Upload 字段则绑定同名的全部文件
// (如 <input multiple>)。字段名由 form: tag 指定。
// Upload receives one multipart uploaded file. It belongs to the request body:
// include an Upload field in a form body struct (decoded via FormBody[T]) to bind
// the file of the same name, or []Upload to bind all files under that name (e.g.
// <input multiple>). The field name comes from the form: tag.
type Upload struct {
	// Filename 是客户端声明的原始文件名(不可信,落盘前须净化)。
	// Filename is the client-declared original file name (untrusted; sanitize
	// before persisting).
	Filename string
	// Size 是文件字节数。
	// Size is the file size in bytes.
	Size int64
	// ContentType 是该 part 的 Content-Type 头(可能为空)。
	// ContentType is the part's Content-Type header (may be empty).
	ContentType string
	// Header 是底层 multipart 文件头,含全部 part 头。
	// Header is the underlying multipart file header, carrying all part headers.
	Header *multipart.FileHeader
	// Open 打开文件内容读取;调用方负责 Close。
	// Open opens the file content for reading; the caller must Close it.
	Open func() (multipart.File, error)
}

// Bytes 读取整个上传文件内容到内存。大文件请改用 Open 流式处理以免占用过多内存。
// Bytes reads the whole uploaded file into memory. For large files prefer Open
// to stream it and avoid excessive memory use.
func (u Upload) Bytes() ([]byte, error) {
	if u.Open == nil {
		return nil, fmt.Errorf("%w: upload not bound", ErrInvalidInput)
	}
	f, err := u.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// Save 把上传文件内容写到 path(截断已存在文件)。它流式拷贝,不整体载入内存。
// path 由调用方确定并须自行净化 Filename,本方法不据 Filename 拼接路径以防目录穿越。
// Save writes the uploaded content to path (truncating an existing file). It
// streams the copy without loading the whole file into memory. The caller
// chooses path and must sanitize Filename; Save never derives the path from
// Filename, to prevent path traversal.
func (u Upload) Save(path string) (err error) {
	if u.Open == nil {
		return fmt.Errorf("%w: upload not bound", ErrInvalidInput)
	}
	src, err := u.Open()
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := dst.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(dst, src)
	return err
}

// uploadFromHeader 从 multipart 文件头构造 Upload(不打开文件,Open 惰性提供)。
// uploadFromHeader builds an Upload from a multipart file header (does not open
// the file; Open provides lazy access).
func uploadFromHeader(h *multipart.FileHeader) Upload {
	return Upload{
		Filename:    h.Filename,
		Size:        h.Size,
		ContentType: h.Header.Get("Content-Type"),
		Header:      h,
		Open:        h.Open,
	}
}
