package fibercase

import "github.com/gofiber/fiber/v3"

func setup() {
	app := fiber.New()
	app.Get("/", handleHome)
	app.Post("/users", handleCreateUser)
	app.Get("/users/:id", handleGetUser)
}

func handleHome(c fiber.Ctx) error      { return nil }
func handleCreateUser(c fiber.Ctx) error { return nil }
func handleGetUser(c fiber.Ctx) error    { return nil }
