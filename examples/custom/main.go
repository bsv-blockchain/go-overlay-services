// Package main demonstrates custom overlay services configuration.
package main

import (
	"log"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
	"github.com/bsv-blockchain/go-overlay-services/pkg/server"
	"github.com/gofiber/fiber/v2"
)

func main() {
	const MB = 1024 * 1024
	app := server.RegisterRoutesWithErrorHandler(fiber.New(), &server.RegisterRoutesConfig{
		ARCAPIKey:        "YOUR_ARC_API_KEY",
		ARCCallbackToken: "YOUR_CALLBACK_TOKEN",
		AdminBearerToken: "YOUR_TOKEN",
		Engine:           engine.NewEngine(engine.Engine{}), // Please remember to define the engine config.
		OctetStreamLimit: 500 * MB,
	})

	if err := app.Listen("localhost:8080"); err != nil {
		log.Fatal(err)
	}
}
