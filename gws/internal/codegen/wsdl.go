package codegen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sofiworker/gk/gws/internal/model"
)

func generateWSDLFile(m model.Model, cfg Config) (string, error) {
	selected, err := selectModel(m, cfg)
	if err != nil {
		return "", err
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf("var %s = &gws.WSDLAssetSet{\n", serviceAssetVarName(primaryServiceName(selected))))
	body.WriteString(fmt.Sprintf("\tMain: []byte(%q),\n", string(cfg.WSDL)))
	body.WriteString("\tXSD: map[string][]byte{\n")

	keys := make([]string, 0, len(cfg.XSD))
	for name := range cfg.XSD {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		body.WriteString(fmt.Sprintf("\t\t%q: []byte(%q),\n", name, string(cfg.XSD[name])))
	}
	body.WriteString("\t},\n")
	body.WriteString("}\n")

	source, err := renderGoFile(cfg.Package, []string{"github.com/sofiworker/gk/gws"}, body.String())
	if err != nil {
		return "", err
	}

	return string(source), nil
}
