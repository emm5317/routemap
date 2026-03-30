package concatpaths

import "github.com/gofiber/fiber/v3"

const apiPrefix = "/api"

func setup() {
	app := fiber.New()
	app.Get(apiPrefix+"/users", handleUsers)
	app.Get("/v1"+"/health", handleHealth)
}

func handleUsers(c fiber.Ctx) error  { return nil }
func handleHealth(c fiber.Ctx) error { return nil }
