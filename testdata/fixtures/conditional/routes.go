package conditional

import "github.com/gin-gonic/gin"

var enableAdmin = true

func setup() {
	r := gin.New()
	r.GET("/", handleHome)

	if enableAdmin {
		r.GET("/admin", handleAdmin)
	} else {
		r.GET("/guest", handleGuest)
	}

	switch "prod" {
	case "prod":
		r.GET("/metrics", handleMetrics)
	case "dev":
		r.GET("/debug", handleDebug)
	}
}

func handleHome(*gin.Context)    {}
func handleAdmin(*gin.Context)   {}
func handleGuest(*gin.Context)   {}
func handleMetrics(*gin.Context) {}
func handleDebug(*gin.Context)   {}
