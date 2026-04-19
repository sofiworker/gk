package main

import (
	"flag"
	"fmt"
	"os"

	soapgen "github.com/sofiworker/gk/gws/generate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("gksoap", flag.ContinueOnError)

	var cfg soapgen.Config
	client := true
	server := true
	embedWSDL := true
	fs.StringVar(&cfg.WSDLPath, "wsdl", "", "path to wsdl file")
	fs.StringVar(&cfg.OutputDir, "o", ".", "output directory")
	fs.StringVar(&cfg.Package, "pkg", "", "generated package name")
	fs.StringVar(&cfg.Service, "service", "", "selected wsdl service name")
	fs.StringVar(&cfg.Port, "port", "", "selected wsdl port name")
	fs.StringVar(&cfg.TypePrefix, "type-prefix", "", "generated type prefix")
	fs.BoolVar(&client, "client", true, "generate typed client code")
	fs.BoolVar(&server, "server", true, "generate server code")
	fs.BoolVar(&embedWSDL, "embed-wsdl", true, "embed wsdl/xsd assets into generated code")
	fs.SetOutput(os.Stderr)

	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg.Client = client
	cfg.Server = server
	cfg.EmbedWSDL = embedWSDL
	cfg.ExplicitOutputFlags = true

	files, err := soapgen.Generate(cfg)
	if err != nil {
		return err
	}

	return soapgen.WriteFiles(files)
}
