package main

import (
	"context"
	"fmt"
	"os"

	"deploy-manager/pkg/assets"
	"deploy-manager/pkg/server"
)

func main() {
	configPath := "config.yaml"
	port := 0
	password := ""

	if len(os.Args) > 1 {
		if os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Println("Usage: deploy-manager [config.yaml]")
			fmt.Println("   or: deploy-manager --port 3001")
			fmt.Println("   or: deploy-manager --port 3001 --password yourpass")
			os.Exit(0)
		}
		i := 1
		for i < len(os.Args) {
			arg := os.Args[i]
			if arg == "-port" || arg == "--port" || arg == "-p" {
				if i+1 < len(os.Args) {
					fmt.Sscanf(os.Args[i+1], "%d", &port)
					i += 2
				}
			} else if arg == "-password" || arg == "--password" || arg == "-P" {
				if i+1 < len(os.Args) {
					password = os.Args[i+1]
					i += 2
				}
			} else {
				configPath = os.Args[i]
				i++
			}
		}
	}

	opts := server.Options{
		Port:              port,
		Password:          password,
		ConfigPath:        configPath,
		WebAssets:         assets.WebAssets,
		RDPAgentExe:       assets.RDPAgentExe,
		DefaultConfig:     assets.DefaultConfig,
		DefaultScenarios:  assets.DefaultScenarios,
		DefaultTemplates:  assets.DefaultTemplates,
	}

	if _, err := server.Start(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
