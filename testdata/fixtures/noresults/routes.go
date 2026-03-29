package noresults

import "github.com/gofiber/fiber/v3"

// Framework is imported but no routes are registered in a way routemap can detect.
func setup() {
	_ = fiber.New()
}
