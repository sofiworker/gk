package codegen

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sofiworker/gk/gws/internal/model"
)

func TestGenerateFiles(t *testing.T) {
	outputDir := t.TempDir()

	files, err := Generate(modelFixture(), Config{
		Package:   "userws",
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	wantNames := []string{
		fileTypes,
		fileOperations,
		fileClient,
		fileHandler,
		fileGServer,
		fileWSDL,
	}

	if len(files) != len(wantNames) {
		t.Fatalf("unexpected generated file count: got=%d want=%d", len(files), len(wantNames))
	}

	gotNames := make([]string, 0, len(files))
	for _, file := range files {
		gotNames = append(gotNames, file.Name)

		if file.Path != filepath.Join(outputDir, file.Name) {
			t.Fatalf("unexpected file path for %q: %q", file.Name, file.Path)
		}
		if len(file.Source) == 0 {
			t.Fatalf("expected %q to have source", file.Name)
		}
		if !bytes.Contains(file.Source, []byte("package userws")) {
			t.Fatalf("expected %q to contain package declaration, got:\n%s", file.Name, file.Source)
		}
	}

	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("unexpected generated file names: got=%v want=%v", gotNames, wantNames)
	}
}

func TestGenerateFilesWithDisabledOutputs(t *testing.T) {
	outputDir := t.TempDir()

	files, err := Generate(modelFixture(), Config{
		Package:             "userws",
		OutputDir:           outputDir,
		Client:              false,
		Server:              true,
		EmbedWSDL:           false,
		ExplicitOutputFlags: true,
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	wantNames := []string{
		fileTypes,
		fileOperations,
		fileHandler,
		fileGServer,
	}

	gotNames := make([]string, 0, len(files))
	for _, file := range files {
		gotNames = append(gotNames, file.Name)
	}

	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("unexpected generated file names: got=%v want=%v", gotNames, wantNames)
	}
}

func TestRenderGoFile(t *testing.T) {
	src, err := renderGoFile("userws", []string{"fmt"}, `
func Example( ) {
fmt.Println("hello")
}
`)
	if err != nil {
		t.Fatalf("renderGoFile returned error: %v", err)
	}

	text := string(src)
	if !strings.Contains(text, "package userws") {
		t.Fatalf("expected package declaration, got:\n%s", text)
	}
	if !strings.Contains(text, "import \"fmt\"") {
		t.Fatalf("expected import block, got:\n%s", text)
	}
	if !strings.Contains(text, "func Example() {") {
		t.Fatalf("expected gofmt output, got:\n%s", text)
	}
}

func modelFixture() model.Model {
	return model.Model{
		TargetNamespace: "urn:user/wsdl",
		Schemas: []model.Schema{
			{
				TargetNamespace: "urn:user/types",
				Elements: []model.Element{
					{
						Name: "CreateUserRequest",
						Type: model.QName{Space: "urn:user/types", Local: "CreateUserRequestType"},
					},
					{
						Name: "CreateUserResponse",
						Type: model.QName{Space: "urn:user/types", Local: "CreateUserResponseType"},
					},
				},
				ComplexTypes: []model.ComplexType{
					{
						Name: "CreateUserRequestType",
						Fields: []model.Field{
							{Name: "name", Type: model.QName{Space: "http://www.w3.org/2001/XMLSchema", Local: "string"}, MinOccurs: 1, MaxOccurs: 1},
						},
					},
					{
						Name: "CreateUserResponseType",
						Fields: []model.Field{
							{Name: "id", Type: model.QName{Space: "http://www.w3.org/2001/XMLSchema", Local: "string"}, MinOccurs: 1, MaxOccurs: 1},
						},
					},
				},
			},
		},
		Messages: []model.Message{
			{
				Name: "CreateUserRequestMessage",
				Parts: []model.MessagePart{
					{Name: "parameters", Element: model.QName{Space: "urn:user/types", Local: "CreateUserRequest"}},
				},
			},
			{
				Name: "CreateUserResponseMessage",
				Parts: []model.MessagePart{
					{Name: "parameters", Element: model.QName{Space: "urn:user/types", Local: "CreateUserResponse"}},
				},
			},
		},
		Bindings: []model.Binding{
			{
				Name:      "UserBinding",
				Type:      model.QName{Space: "urn:user/wsdl", Local: "UserPortType"},
				Style:     "document",
				Transport: "http://schemas.xmlsoap.org/soap/http",
				Operations: []model.BindingOperation{
					{
						Name:          "CreateUser",
						Action:        "urn:user:CreateUser",
						Style:         "document",
						InputMessage:  model.QName{Space: "urn:user/wsdl", Local: "CreateUserRequestMessage"},
						OutputMessage: model.QName{Space: "urn:user/wsdl", Local: "CreateUserResponseMessage"},
						InputUse:      "literal",
						OutputUse:     "literal",
					},
				},
			},
		},
		Services: []model.Service{
			{
				Name: "UserService",
				Ports: []model.Port{
					{
						Name:    "UserServicePort",
						Binding: model.QName{Space: "urn:user/wsdl", Local: "UserBinding"},
						Address: "http://localhost:8080/user",
					},
				},
			},
		},
	}
}

func multiServiceFixture() model.Model {
	m := modelFixture()

	adminSchema := model.Schema{
		TargetNamespace: "urn:admin/types",
		Elements: []model.Element{
			{
				Name: "DeleteUserRequest",
				Type: model.QName{Space: "urn:admin/types", Local: "DeleteUserRequestType"},
			},
			{
				Name: "DeleteUserResponse",
				Type: model.QName{Space: "urn:admin/types", Local: "DeleteUserResponseType"},
			},
		},
		ComplexTypes: []model.ComplexType{
			{
				Name: "DeleteUserRequestType",
				Fields: []model.Field{
					{Name: "id", Type: model.QName{Space: "http://www.w3.org/2001/XMLSchema", Local: "string"}, MinOccurs: 1, MaxOccurs: 1},
				},
			},
			{
				Name: "DeleteUserResponseType",
				Fields: []model.Field{
					{Name: "deleted", Type: model.QName{Space: "http://www.w3.org/2001/XMLSchema", Local: "boolean"}, MinOccurs: 1, MaxOccurs: 1},
				},
			},
		},
	}

	m.Schemas = append(m.Schemas, adminSchema)
	m.Messages = append(m.Messages,
		model.Message{
			Name: "DeleteUserRequestMessage",
			Parts: []model.MessagePart{
				{Name: "parameters", Element: model.QName{Space: "urn:admin/types", Local: "DeleteUserRequest"}},
			},
		},
		model.Message{
			Name: "DeleteUserResponseMessage",
			Parts: []model.MessagePart{
				{Name: "parameters", Element: model.QName{Space: "urn:admin/types", Local: "DeleteUserResponse"}},
			},
		},
	)
	m.Bindings = append(m.Bindings, model.Binding{
		Name:      "AdminBinding",
		Type:      model.QName{Space: "urn:user/wsdl", Local: "AdminPortType"},
		Style:     "document",
		Transport: "http://schemas.xmlsoap.org/soap/http",
		Operations: []model.BindingOperation{
			{
				Name:          "DeleteUser",
				Action:        "urn:admin:DeleteUser",
				Style:         "document",
				InputMessage:  model.QName{Space: "urn:user/wsdl", Local: "DeleteUserRequestMessage"},
				OutputMessage: model.QName{Space: "urn:user/wsdl", Local: "DeleteUserResponseMessage"},
				InputUse:      "literal",
				OutputUse:     "literal",
			},
		},
	})
	m.Services = append(m.Services, model.Service{
		Name: "AdminService",
		Ports: []model.Port{
			{
				Name:    "AdminPort",
				Binding: model.QName{Space: "urn:user/wsdl", Local: "AdminBinding"},
				Address: "http://localhost:8080/admin",
			},
		},
	})

	return m
}
