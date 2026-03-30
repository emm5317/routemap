package helperfunc

import "github.com/gin-gonic/gin"

func main() {
	r := gin.New()
	r.GET("/", handleHome)
	setupAPI(r)
}

func setupAPI(r *gin.Engine) {
	r.GET("/api/users", handleUsers)
	r.POST("/api/users", createUser)
}

func handleHome(c *gin.Context)  {}
func handleUsers(c *gin.Context) {}
func createUser(c *gin.Context)  {}
