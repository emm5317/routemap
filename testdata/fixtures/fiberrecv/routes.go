package fiberrecv

import "github.com/gofiber/fiber/v3"

type App struct {
	app *fiber.App
}

func New() *App {
	a := &App{app: fiber.New()}
	a.routes()
	return a
}

func (a *App) routes() {
	a.app.Get("/", a.handleHome)
	a.app.Post("/bets/:id/settle", a.handleSettle)
}

func (a *App) handleHome(c fiber.Ctx) error   { return nil }
func (a *App) handleSettle(c fiber.Ctx) error  { return nil }
