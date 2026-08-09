package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/sofiworker/gk/ghttp/testdata/k6/internal/rawprobe"
	"os"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "server address")
	timeout := flag.Duration("timeout", 60*time.Second, "probe timeout")
	output := flag.String("output", "", "output file")
	secret := flag.String("secret", "", "configured secret")
	healthURL := flag.String("health-url", "", "health probe URL")
	metricsURL := flag.String("metrics-url", "", "metrics probe URL")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	cases := rawprobe.DefaultCases(*secret, 1<<20)
	rep := rawprobe.RunWithConfig(ctx, *addr, cases, rawprobe.Config{HealthURL: *healthURL, MetricsURL: *metricsURL})
	b, _ := json.MarshalIndent(rep, "", "  ")
	if *output != "" {
		if err := os.WriteFile(*output, b, 0644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	} else {
		fmt.Println(string(b))
	}
	if rep.Failed > 0 {
		for _, r := range rep.Results {
			if r.Class == "health_failure" || r.Class == "recovery_failure" {
				os.Exit(5)
			}
		}
		allConn := len(rep.Results) > 0
		for _, r := range rep.Results {
			if r.Class != "connection" {
				allConn = false
			}
		}
		if allConn {
			os.Exit(3)
		}
		os.Exit(4)
	}
}
