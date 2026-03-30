package crossfile

import "github.com/gin-gonic/gin"

func setupAPI(r *gin.Engine) {
	r.GET("/api/users", handleUsers)
	r.POST("/api/users", createUser)
}

func handleUsers(c *gin.Context) {}
func createUser(c *gin.Context)  {}
