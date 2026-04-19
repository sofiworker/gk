package codegen

import (
	"fmt"
	"strings"

	"github.com/sofiworker/gk/gws/internal/model"
)

func generateClientFile(m model.Model, cfg Config) (string, error) {
	selected, err := selectModel(m, cfg)
	if err != nil {
		return "", err
	}

	serviceName := primaryServiceName(selected)
	clientName := typeName(serviceName, "") + "Client"

	operations, err := listOperations(selected)
	if err != nil {
		return "", err
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf("type %s struct {\n", clientName))
	body.WriteString("\tclient   *gws.Client\n")
	body.WriteString("\tendpoint string\n")
	body.WriteString("}\n\n")

	body.WriteString(fmt.Sprintf("func New%s(endpoint string, opts ...gws.ClientOption) *%s {\n", clientName, clientName))
	body.WriteString(fmt.Sprintf("\treturn &%s{\n", clientName))
	body.WriteString("\t\tclient:   gws.NewClient(opts...),\n")
	body.WriteString("\t\tendpoint: endpoint,\n")
	body.WriteString("\t}\n")
	body.WriteString("}\n\n")

	body.WriteString(fmt.Sprintf("func (c *%s) Client() *gws.Client {\n", clientName))
	body.WriteString("\tif c == nil {\n")
	body.WriteString("\t\treturn nil\n")
	body.WriteString("\t}\n")
	body.WriteString("\treturn c.client\n")
	body.WriteString("}\n\n")

	body.WriteString(fmt.Sprintf("func (c *%s) Endpoint() string {\n", clientName))
	body.WriteString("\tif c == nil {\n")
	body.WriteString("\t\treturn \"\"\n")
	body.WriteString("\t}\n")
	body.WriteString("\treturn c.endpoint\n")
	body.WriteString("}\n\n")

	body.WriteString(fmt.Sprintf("func (c *%s) SetEndpoint(endpoint string) *%s {\n", clientName, clientName))
	body.WriteString("\tif c == nil {\n")
	body.WriteString("\t\treturn nil\n")
	body.WriteString("\t}\n")
	body.WriteString("\tc.endpoint = endpoint\n")
	body.WriteString("\treturn c\n")
	body.WriteString("}\n\n")

	for _, operation := range operations {
		requestType := typeName(operation.RequestWrapper.Local, cfg.TypePrefix)
		responseType := typeName(operation.ResponseWrapper.Local, cfg.TypePrefix)
		methodName := typeName(operation.Name, "")

		body.WriteString(fmt.Sprintf(
			"func (c *%s) New%sRequest(ctx context.Context, in *%s) (*gws.Request, error) {\n",
			clientName,
			methodName,
			requestType,
		))
		body.WriteString(fmt.Sprintf("\treq := gws.NewRequest(ctx, c.endpoint, %sOperation())\n", methodName))
		body.WriteString("\treq.SetBody(in)\n")
		body.WriteString("\treturn req, nil\n")
		body.WriteString("}\n\n")

		body.WriteString(fmt.Sprintf(
			"func (c *%s) %sRaw(ctx context.Context, in *%s) ([]byte, error) {\n",
			clientName,
			methodName,
			requestType,
		))
		body.WriteString(fmt.Sprintf("\treq, err := c.New%sRequest(ctx, in)\n", methodName))
		body.WriteString("\tif err != nil {\n")
		body.WriteString("\t\treturn nil, err\n")
		body.WriteString("\t}\n")
		body.WriteString("\treturn c.client.DoRaw(req)\n")
		body.WriteString("}\n\n")

		body.WriteString(fmt.Sprintf(
			"func (c *%s) %s(ctx context.Context, in *%s) (*%s, error) {\n",
			clientName,
			methodName,
			requestType,
			responseType,
		))
		body.WriteString(fmt.Sprintf("\treq, err := c.New%sRequest(ctx, in)\n", methodName))
		body.WriteString("\tif err != nil {\n")
		body.WriteString("\t\treturn nil, err\n")
		body.WriteString("\t}\n")
		body.WriteString(fmt.Sprintf("\tout := &%s{}\n", responseType))
		body.WriteString("\tif err := c.client.Do(req, out); err != nil {\n")
		body.WriteString("\t\treturn nil, err\n")
		body.WriteString("\t}\n")
		body.WriteString("\treturn out, nil\n")
		body.WriteString("}\n\n")
	}

	source, err := renderGoFile(cfg.Package, []string{"context", "github.com/sofiworker/gk/gws"}, body.String())
	if err != nil {
		return "", err
	}

	return string(source), nil
}

type operationMetadata struct {
	Name            string
	RequestWrapper  model.QName
	ResponseWrapper model.QName
}

func listOperations(m model.Model) ([]operationMetadata, error) {
	seen := make(map[string]struct{})
	operations := make([]operationMetadata, 0)

	for _, binding := range m.Bindings {
		for _, operation := range binding.Operations {
			if _, ok := seen[operation.Name]; ok {
				continue
			}

			requestWrapper, responseWrapper, err := findOperationWrappers(m, operation)
			if err != nil {
				return nil, err
			}

			operations = append(operations, operationMetadata{
				Name:            operation.Name,
				RequestWrapper:  requestWrapper,
				ResponseWrapper: responseWrapper,
			})
			seen[operation.Name] = struct{}{}
		}
	}

	return operations, nil
}

func primaryServiceName(m model.Model) string {
	if len(m.Services) > 0 && strings.TrimSpace(m.Services[0].Name) != "" {
		return m.Services[0].Name
	}
	if len(m.Bindings) > 0 && strings.TrimSpace(m.Bindings[0].Name) != "" {
		return m.Bindings[0].Name
	}
	return "Service"
}
