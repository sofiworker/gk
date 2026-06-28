package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGeneratedPackageCompileAndRoundTrip(t *testing.T) {
	runGeneratedPackageTest(t, "TestGeneratedRoundTrip", generatedRoundTripTestSource())
}

func TestGeneratedPackageFault(t *testing.T) {
	runGeneratedPackageTest(t, "TestGeneratedFault", generatedFaultTestSource())
}

func TestGeneratedPackageWSDL(t *testing.T) {
	runGeneratedPackageTest(t, "TestGeneratedWSDL", generatedWSDLTestSource())
}

func runGeneratedPackageTest(t *testing.T, testName, testSource string) {
	t.Helper()

	repoRoot := repoRoot(t)
	dir, err := os.MkdirTemp(repoRoot, ".tmp_codegen_")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	runGenerateFixture(t, repoRoot, dir, "echo.wsdl", "echows")

	testFile := filepath.Join(dir, "e2e_generated_test.go")
	if err := os.WriteFile(testFile, []byte(testSource), 0o644); err != nil {
		t.Fatalf("write generated test file: %v", err)
	}

	relDir, err := filepath.Rel(repoRoot, dir)
	if err != nil {
		t.Fatalf("resolve relative dir: %v", err)
	}

	cmd := exec.Command("go", "test", "-run", testName, "./"+filepath.ToSlash(relDir))
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test generated package failed: %v\n%s", err, output)
	}
}

func runGenerateFixture(t *testing.T, repoRoot, outputDir, fixtureName, pkg string) {
	t.Helper()

	wsdlPath := filepath.Join("gws", "testdata", "wsdl", fixtureName)
	cmd := exec.Command("go", "run", "./cmd/gksoap", "-wsdl", wsdlPath, "-o", outputDir, "-pkg", pkg)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run gksoap failed: %v\n%s", err, output)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve current file path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func generatedRoundTripTestSource() string {
	return `package echows

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripServer struct{}

func (roundTripServer) Echo(ctx context.Context, req *EchoRequest) (*EchoResponse, error) {
	return &EchoResponse{Message: "hello " + req.Message}, nil
}

type switchedEndpointServer struct{}

func (switchedEndpointServer) Echo(ctx context.Context, req *EchoRequest) (*EchoResponse, error) {
	return &EchoResponse{Message: "switched " + req.Message}, nil
}

func TestGeneratedRoundTrip(t *testing.T) {
	op := EchoOperation()
	if op.Name != "Echo" {
		t.Fatalf("unexpected operation name: %q", op.Name)
	}
	if op.RequestWrapper != (xml.Name{Space: "urn:echo/types", Local: "EchoRequest"}) {
		t.Fatalf("unexpected request wrapper: %#v", op.RequestWrapper)
	}

	desc := EchoServiceDesc()
	if desc == nil {
		t.Fatal("EchoServiceDesc returned nil")
	}
	if desc.Name != "EchoService" {
		t.Fatalf("unexpected service desc name: %q", desc.Name)
	}
	if len(desc.Operations) != 1 {
		t.Fatalf("unexpected operation count: %d", len(desc.Operations))
	}
	if desc.Operations[0].Invoke != nil {
		t.Fatal("service desc accessor should not bind invoke implementation")
	}
	reqValue := desc.Operations[0].NewRequestValue()
	if _, ok := reqValue.(*EchoRequest); !ok {
		t.Fatalf("unexpected request factory type: %T", reqValue)
	}

	h, err := NewEchoServiceHandler(roundTripServer{})
	if err != nil {
		t.Fatalf("NewEchoServiceHandler returned error: %v", err)
	}

	srv := httptest.NewServer(h)
	defer srv.Close()

	client := NewEchoServiceClient(srv.URL)
	if client.Client() == nil {
		t.Fatal("Client accessor returned nil")
	}
	if client.Endpoint() != srv.URL {
		t.Fatalf("unexpected initial endpoint: %q", client.Endpoint())
	}

	raw, err := client.EchoRaw(context.Background(), &EchoRequest{Message: "soap"})
	if err != nil {
		t.Fatalf("EchoRaw returned error: %v", err)
	}
	if !strings.Contains(string(raw), "EchoResponse") {
		t.Fatalf("unexpected raw response: %s", raw)
	}

	resp, err := client.Echo(context.Background(), &EchoRequest{Message: "soap"})
	if err != nil {
		t.Fatalf("Echo returned error: %v", err)
	}
	if resp.Message != "hello soap" {
		t.Fatalf("unexpected response message: %q", resp.Message)
	}

	switched := httptest.NewServer(NewEchoServiceHandlerMust(switchedEndpointServer{}, t))
	defer switched.Close()

	ret := client.SetEndpoint(switched.URL)
	if ret != client {
		t.Fatal("SetEndpoint should return client itself")
	}
	if client.Endpoint() != switched.URL {
		t.Fatalf("unexpected switched endpoint: %q", client.Endpoint())
	}

	switchedResp, err := client.Echo(context.Background(), &EchoRequest{Message: "soap"})
	if err != nil {
		t.Fatalf("Echo after endpoint switch returned error: %v", err)
	}
	if switchedResp.Message != "switched soap" {
		t.Fatalf("unexpected switched response message: %q", switchedResp.Message)
	}
}

func NewEchoServiceHandlerMust(impl EchoServiceServer, t *testing.T) http.Handler {
	t.Helper()
	h, err := NewEchoServiceHandler(impl)
	if err != nil {
		t.Fatalf("NewEchoServiceHandler returned error: %v", err)
	}
	return h
}
`
}

func generatedFaultTestSource() string {
	return `package echows

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/sofiworker/gk/gws"
)

type faultServer struct{}

func (faultServer) Echo(ctx context.Context, req *EchoRequest) (*EchoResponse, error) {
	return nil, &gws.Fault{
		Code:   "soap:Client",
		String: "boom",
	}
}

func TestGeneratedFault(t *testing.T) {
	h, err := NewEchoServiceHandler(faultServer{})
	if err != nil {
		t.Fatalf("NewEchoServiceHandler returned error: %v", err)
	}

	srv := httptest.NewServer(h)
	defer srv.Close()

	client := NewEchoServiceClient(srv.URL)
	_, err = client.Echo(context.Background(), &EchoRequest{Message: "soap"})
	if err == nil {
		t.Fatal("expected fault error")
	}

	var faultErr *gws.FaultError
	if !errors.As(err, &faultErr) {
		t.Fatalf("expected FaultError, got: %v", err)
	}
	if faultErr.Fault.String != "boom" {
		t.Fatalf("unexpected fault string: %q", faultErr.Fault.String)
	}
}
`
}

func generatedWSDLTestSource() string {
	return fmt.Sprintf(`package echows

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type wsdlServer struct{}

func (wsdlServer) Echo(ctx context.Context, req *EchoRequest) (*EchoResponse, error) {
	return &EchoResponse{Message: req.Message}, nil
}

func TestGeneratedWSDL(t *testing.T) {
	h, err := NewEchoServiceHandler(wsdlServer{})
	if err != nil {
		t.Fatalf("NewEchoServiceHandler returned error: %%v", err)
	}

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "?wsdl")
	if err != nil {
		t.Fatalf("GET wsdl returned error: %%v", err)
	}
	defer resp.Body.Close()

	wsdlData, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read wsdl body: %%v", err)
	}
	if !bytes.Contains(wsdlData, []byte("<wsdl:definitions")) {
		t.Fatalf("unexpected wsdl body: %%s", wsdlData)
	}

	resp, err = http.Get(srv.URL + "?xsd=%s")
	if err != nil {
		t.Fatalf("GET xsd returned error: %%v", err)
	}
	defer resp.Body.Close()

	xsdData, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read xsd body: %%v", err)
	}
	if !bytes.Contains(xsdData, []byte("<xsd:schema")) {
		t.Fatalf("unexpected xsd body: %%s", xsdData)
	}
}
`, "echo.xsd")
}
