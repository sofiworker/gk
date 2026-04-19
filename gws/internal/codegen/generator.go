package codegen

import (
	"errors"
	"fmt"

	"github.com/sofiworker/gk/gws/internal/model"
)

var ErrEmptyPackage = errors.New("empty package")
var ErrServiceNotFound = errors.New("service not found")
var ErrPortNotFound = errors.New("port not found")
var ErrBindingNotFound = errors.New("binding not found")

type Config struct {
	Package             string
	OutputDir           string
	TypePrefix          string
	Service             string
	Port                string
	Client              bool
	Server              bool
	EmbedWSDL           bool
	ExplicitOutputFlags bool
	WSDL                []byte
	XSD                 map[string][]byte
}

func Generate(m model.Model, cfg Config) ([]GeneratedFile, error) {
	if cfg.Package == "" {
		return nil, ErrEmptyPackage
	}

	cfg = normalizeConfig(cfg)

	builders := []struct {
		name   string
		render func(model.Model, Config) (string, error)
	}{{name: fileTypes, render: generateTypesFile}, {name: fileOperations, render: generateOperationsFile}}

	if cfg.Client {
		builders = append(builders, struct {
			name   string
			render func(model.Model, Config) (string, error)
		}{name: fileClient, render: generateClientFile})
	}
	if cfg.Server {
		builders = append(builders, struct {
			name   string
			render func(model.Model, Config) (string, error)
		}{name: fileHandler, render: generateHandlerFile})
		builders = append(builders, struct {
			name   string
			render func(model.Model, Config) (string, error)
		}{name: fileGServer, render: generateGServerFile})
		if cfg.EmbedWSDL {
			builders = append(builders, struct {
				name   string
				render func(model.Model, Config) (string, error)
			}{name: fileWSDL, render: generateWSDLFile})
		}
	}

	files := make([]GeneratedFile, 0, len(builders))
	for _, builder := range builders {
		rendered, err := builder.render(m, cfg)
		if err != nil {
			return nil, fmt.Errorf("generate %s: %w", builder.name, err)
		}

		files = append(files, newGeneratedFile(cfg.OutputDir, builder.name, []byte(rendered)))
	}

	return files, nil
}

func stubFileRenderer(subject string) func(model.Model, Config) (string, error) {
	return func(m model.Model, cfg Config) (string, error) {
		body := fmt.Sprintf("// %s.", subject)
		if m.TargetNamespace != "" {
			body = fmt.Sprintf("// %s for %q.", subject, m.TargetNamespace)
		}

		source, err := renderGoFile(cfg.Package, nil, body)
		if err != nil {
			return "", err
		}

		return string(source), nil
	}
}

func normalizeConfig(cfg Config) Config {
	if !cfg.ExplicitOutputFlags {
		cfg.Client = true
		cfg.Server = true
		cfg.EmbedWSDL = true
	}
	if cfg.Server {
		if !cfg.ExplicitOutputFlags && !cfg.EmbedWSDL {
			cfg.EmbedWSDL = true
		}
	} else {
		cfg.EmbedWSDL = false
	}

	return cfg
}

func selectModel(m model.Model, cfg Config) (model.Model, error) {
	service, err := selectService(m, cfg.Service)
	if err != nil {
		return model.Model{}, err
	}

	port, err := selectPort(service, cfg.Port)
	if err != nil {
		return model.Model{}, err
	}

	binding, err := selectBinding(m, port.Binding)
	if err != nil {
		return model.Model{}, err
	}

	service.Ports = []model.Port{port}
	m.Services = []model.Service{service}
	m.Bindings = []model.Binding{binding}

	return m, nil
}

func selectService(m model.Model, name string) (model.Service, error) {
	if len(m.Services) == 0 {
		return model.Service{}, ErrServiceNotFound
	}

	if name == "" {
		return m.Services[0], nil
	}

	for _, service := range m.Services {
		if service.Name == name {
			return service, nil
		}
	}

	return model.Service{}, fmt.Errorf("%w: %s", ErrServiceNotFound, name)
}

func selectPort(service model.Service, name string) (model.Port, error) {
	if len(service.Ports) == 0 {
		return model.Port{}, ErrPortNotFound
	}

	if name == "" {
		return service.Ports[0], nil
	}

	for _, port := range service.Ports {
		if port.Name == name {
			return port, nil
		}
	}

	return model.Port{}, fmt.Errorf("%w: %s", ErrPortNotFound, name)
}

func selectBinding(m model.Model, name model.QName) (model.Binding, error) {
	for _, binding := range m.Bindings {
		if binding.Name == name.Local {
			return binding, nil
		}
	}

	return model.Binding{}, fmt.Errorf("%w: %s", ErrBindingNotFound, name.Local)
}
