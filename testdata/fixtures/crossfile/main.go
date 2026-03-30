package crossfile

import "github.com/gin-gonic/gin"

func main() {
	r := gin.New()
	r.GET("/", handleHome)
	setupAPI(r)
}

func handleHome(c *gin.Context) {}
