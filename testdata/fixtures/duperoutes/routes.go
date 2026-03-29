package duperoutes

import "github.com/gin-gonic/gin"

func setup() {
	r := gin.New()
	r.GET("/users", handleUsersV1)
	r.GET("/users", handleUsersV2)
	r.POST("/items", handleCreateItem)
}

func handleUsersV1(*gin.Context)    {}
func handleUsersV2(*gin.Context)    {}
func handleCreateItem(*gin.Context) {}
