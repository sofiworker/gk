package codegen

import (
	"fmt"
	"strings"

	"github.com/sofiworker/gk/gws/internal/model"
)

func generateGServerFile(m model.Model, cfg Config) (string, error) {
	selected, err := selectModel(m, cfg)
	if err != nil {
		return "", err
	}

	serviceName := primaryServiceName(selected)
	serverName := typeName(serviceName, "") + "Server"
	registerName := "Register" + typeName(serviceName, "") + "Server"

	var body strings.Builder
	body.WriteString(fmt.Sprintf(
		"func %s(s *httpserver.Server, path string, impl %s, opts ...gws.ServiceOption) error {\n",
		registerName,
		serverName,
	))
	body.WriteString("\th, err := New" + typeName(serviceName, "") + "Handler(impl, opts...)\n")
	body.WriteString("\tif err != nil {\n")
	body.WriteString("\t\treturn err\n")
	body.WriteString("\t}\n")
	body.WriteString("\treturn adaptergserver.Register(s, path, h)\n")
	body.WriteString("}\n")

	source, err := renderGoFile(
		cfg.Package,
		[]string{
			"github.com/sofiworker/gk/ghttp/gserver",
			"github.com/sofiworker/gk/gws",
			"github.com/sofiworker/gk/gws/adapter/gserver",
		},
		body.String(),
	)
	if err != nil {
		return "", err
	}

	sourceText := string(source)
	sourceText = strings.ReplaceAll(sourceText, "\"github.com/sofiworker/gk/ghttp/gserver\"", "httpserver \"github.com/sofiworker/gk/ghttp/gserver\"")
	sourceText = strings.ReplaceAll(sourceText, "\"github.com/sofiworker/gk/gws/adapter/gserver\"", "adaptergserver \"github.com/sofiworker/gk/gws/adapter/gserver\"")
	return sourceText, nil
}
