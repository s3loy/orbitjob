package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	_ = r.Run(":8080")
}
