package constpaths

import "github.com/gofiber/fiber/v3"

const (
	HealthPath = "/health"
	UsersPath  = "/users"
)

func setup() {
	app := fiber.New()
	app.Get(HealthPath, handleHealth)
	app.Get(UsersPath, handleUsers)
}

func handleHealth(c fiber.Ctx) error { return nil }
func handleUsers(c fiber.Ctx) error  { return nil }
