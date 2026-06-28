package generate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sofiworker/gk/gws/internal/codegen"
	"github.com/sofiworker/gk/gws/internal/wsdl"
)

var ErrEmptyWSDLPath = errors.New("empty wsdl path")

type Config struct {
	WSDLPath            string
	OutputDir           string
	Package             string
	TypePrefix          string
	Service             string
	Port                string
	Client              bool
	Server              bool
	EmbedWSDL           bool
	ExplicitOutputFlags bool
}

type GeneratedFile struct {
	Name   string
	Path   string
	Source []byte
}

func Generate(cfg Config) ([]GeneratedFile, error) {
	wsdlPath := strings.TrimSpace(cfg.WSDLPath)
	if wsdlPath == "" {
		return nil, ErrEmptyWSDLPath
	}

	m, err := wsdl.ParseFile(wsdlPath)
	if err != nil {
		return nil, fmt.Errorf("parse wsdl: %w", err)
	}

	mainData, xsdAssets, err := wsdl.LoadLocalAssets(wsdlPath)
	if err != nil {
		return nil, fmt.Errorf("load wsdl assets: %w", err)
	}

	files, err := codegen.Generate(m, codegen.Config{
		Package:             cfg.Package,
		OutputDir:           cfg.OutputDir,
		TypePrefix:          cfg.TypePrefix,
		Service:             cfg.Service,
		Port:                cfg.Port,
		Client:              cfg.Client,
		Server:              cfg.Server,
		EmbedWSDL:           cfg.EmbedWSDL,
		ExplicitOutputFlags: cfg.ExplicitOutputFlags,
		WSDL:                mainData,
		XSD:                 xsdAssets,
	})
	if err != nil {
		return nil, fmt.Errorf("generate code: %w", err)
	}

	out := make([]GeneratedFile, 0, len(files))
	for _, file := range files {
		out = append(out, GeneratedFile{
			Name:   file.Name,
			Path:   file.Path,
			Source: append([]byte(nil), file.Source...),
		})
	}
	return out, nil
}

func WriteFiles(files []GeneratedFile) error {
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.Path), 0o755); err != nil {
			return fmt.Errorf("create output dir for %q: %w", file.Path, err)
		}
		if err := os.WriteFile(file.Path, file.Source, 0o644); err != nil {
			return fmt.Errorf("write generated file %q: %w", file.Path, err)
		}
	}

	return nil
}
