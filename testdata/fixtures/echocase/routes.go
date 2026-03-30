package echocase

import "github.com/labstack/echo/v4"

func setup() {
	e := echo.New()
	e.Use(LogMW)

	e.GET("/", handleHome)

	api := e.Group("/api", APIMW)
	api.GET("/items", handleListItems)
	api.POST("/items", handleCreateItem)
}

func LogMW(next echo.HandlerFunc) echo.HandlerFunc  { return next }
func APIMW(next echo.HandlerFunc) echo.HandlerFunc  { return next }
func handleHome(c echo.Context) error              { return nil }
func handleListItems(c echo.Context) error         { return nil }
func handleCreateItem(c echo.Context) error        { return nil }
