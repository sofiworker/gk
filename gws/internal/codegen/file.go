package codegen

import "path/filepath"

const (
	fileTypes      = "types_gen.go"
	fileOperations = "operations_gen.go"
	fileClient     = "client_gen.go"
	fileHandler    = "handler_gen.go"
	fileGServer    = "gserver_gen.go"
	fileWSDL       = "wsdl_gen.go"
)

type GeneratedFile struct {
	Name   string
	Path   string
	Source []byte
}

func newGeneratedFile(outputDir, name string, source []byte) GeneratedFile {
	path := name
	if outputDir != "" {
		path = filepath.Join(outputDir, name)
	}

	return GeneratedFile{
		Name:   name,
		Path:   path,
		Source: source,
	}
}
