package wsdl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LoadLocalAssets(path string) ([]byte, map[string][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read wsdl file %q: %w", path, err)
	}

	collector := assetCollector{
		loader: newLocalLoader(),
		xsd:    make(map[string][]byte),
	}
	if err := collector.collect(filepath.Dir(path), data); err != nil {
		return nil, nil, err
	}

	return data, collector.xsd, nil
}

type assetCollector struct {
	loader *localLoader
	xsd    map[string][]byte
}

func (c *assetCollector) collect(baseDir string, data []byte) error {
	root, err := parseXMLNode(data)
	if err != nil {
		return fmt.Errorf("parse xml assets: %w", err)
	}

	return c.collectNode(baseDir, root)
}

func (c *assetCollector) collectNode(baseDir string, node *xmlNode) error {
	if node == nil {
		return nil
	}

	if schemaLocation := strings.TrimSpace(attr(node, "schemaLocation")); schemaLocation != "" {
		data, nextBaseDir, loaded, err := c.loader.load(baseDir, schemaLocation)
		if err != nil {
			return fmt.Errorf("load schema asset %q: %w", schemaLocation, err)
		}
		if loaded {
			c.xsd[filepath.ToSlash(schemaLocation)] = data
			if err := c.collect(nextBaseDir, data); err != nil {
				return err
			}
		}
	}

	for _, child := range node.Children {
		if err := c.collectNode(baseDir, child); err != nil {
			return err
		}
	}

	return nil
}
