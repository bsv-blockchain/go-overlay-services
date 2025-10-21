// Package main generates a default overlay services configuration file.
package main

import (
	"flag"
	"log"

	"github.com/bsv-blockchain/go-overlay-services/pkg/server/config"
	"github.com/google/uuid"
)

func main() {
	regenToken := flag.Bool("regen-token", false, "Regenerate admin bearer token")
	flag.BoolVar(regenToken, "t", false, "Regenerate admin bearer token (shorthand)")

	outputFile := flag.String("output-file", "config.yaml", "Output configuration file path")
	flag.StringVar(outputFile, "o", "config.yaml", "Output configuration file path (shorthand)")
	flag.Parse()

	cfg := config.NewDefault()
	if *regenToken {
		cfg.Server.AdminBearerToken = uuid.NewString()
	}

	err := cfg.Export(*outputFile)
	if err != nil {
		log.Fatalf("Error writing configuration: %v\n", err)
	}

	log.Printf("Configuration written to %s\n", *outputFile)
}
