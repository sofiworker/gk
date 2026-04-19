package codegen

import (
	"fmt"
	"strings"

	"github.com/sofiworker/gk/gws/internal/model"
)

func generateHandlerFile(m model.Model, cfg Config) (string, error) {
	selected, err := selectModel(m, cfg)
	if err != nil {
		return "", err
	}

	serviceName := primaryServiceName(selected)
	serverName := typeName(serviceName, "") + "Server"

	operations, err := listOperations(selected)
	if err != nil {
		return "", err
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf("type %s interface {\n", serverName))
	for _, operation := range operations {
		methodName := typeName(operation.Name, "")
		requestType := typeName(operation.RequestWrapper.Local, cfg.TypePrefix)
		responseType := typeName(operation.ResponseWrapper.Local, cfg.TypePrefix)
		body.WriteString(fmt.Sprintf("\t%s(ctx context.Context, req *%s) (*%s, error)\n", methodName, requestType, responseType))
	}
	body.WriteString("}\n\n")

	descAccessorName := typeName(serviceName, "") + "Desc"
	body.WriteString(fmt.Sprintf("func %s() *gws.ServiceDesc {\n", descAccessorName))
	body.WriteString("\treturn &gws.ServiceDesc{\n")
	body.WriteString(fmt.Sprintf("\t\tName: %q,\n", serviceName))
	if cfg.EmbedWSDL {
		body.WriteString(fmt.Sprintf("\t\tWSDL: %s,\n", serviceAssetVarName(serviceName)))
	}
	body.WriteString("\t\tOperations: []gws.OperationDesc{\n")
	for _, operation := range operations {
		methodName := typeName(operation.Name, "")
		requestType := typeName(operation.RequestWrapper.Local, cfg.TypePrefix)
		responseType := typeName(operation.ResponseWrapper.Local, cfg.TypePrefix)

		body.WriteString("\t\t\t{\n")
		body.WriteString(fmt.Sprintf("\t\t\t\tOperation: %sOperation(),\n", methodName))
		body.WriteString(fmt.Sprintf("\t\t\t\tNewRequest: func() any { return &%s{} },\n", requestType))
		body.WriteString(fmt.Sprintf("\t\t\t\tNewResponse: func() any { return &%s{} },\n", responseType))
		body.WriteString("\t\t\t},\n")
	}
	body.WriteString("\t\t},\n")
	body.WriteString("\t}\n")
	body.WriteString("}\n\n")

	handlerName := "New" + typeName(serviceName, "") + "Handler"
	body.WriteString(fmt.Sprintf("func %s(impl %s, opts ...gws.ServiceOption) (http.Handler, error) {\n", handlerName, serverName))
	body.WriteString(fmt.Sprintf("\tdesc := %s()\n", descAccessorName))
	for index, operation := range operations {
		methodName := typeName(operation.Name, "")
		requestType := typeName(operation.RequestWrapper.Local, cfg.TypePrefix)
		body.WriteString(fmt.Sprintf("\tdesc.Operations[%d].Invoke = func(ctx context.Context, _ any, req any) (any, error) {\n", index))
		body.WriteString(fmt.Sprintf("\t\ttypedReq, _ := req.(*%s)\n", requestType))
		body.WriteString(fmt.Sprintf("\t\treturn impl.%s(ctx, typedReq)\n", methodName))
		body.WriteString("\t}\n")
	}
	body.WriteString("\treturn gws.NewHandler(desc, impl, opts...)\n")
	body.WriteString("}\n")

	source, err := renderGoFile(
		cfg.Package,
		[]string{"context", "net/http", "github.com/sofiworker/gk/gws"},
		body.String(),
	)
	if err != nil {
		return "", err
	}

	return string(source), nil
}
