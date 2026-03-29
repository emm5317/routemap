package gincase

import "github.com/gin-gonic/gin"

func setup() {
	r := gin.New()
	r.Use(Auth, Log)
	api := r.Group("/api")
	api.Use(APIMW)
	api.GET("/users", RouteMW, getUsers)
}

func Auth(*gin.Context)     {}
func Log(*gin.Context)      {}
func APIMW(*gin.Context)    {}
func RouteMW(*gin.Context)  {}
func getUsers(*gin.Context) {}
