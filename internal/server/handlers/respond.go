package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RespondJSON writes a status code and JSON payload to the context.
func RespondJSON(c *gin.Context, status int, data any) {
	c.JSON(status, data)
}

// RespondOK writes a 200 OK status code and JSON payload to the context.
func RespondOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, data)
}

// RespondCreated writes a 201 Created status code and JSON payload to the context.
func RespondCreated(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, data)
}

// RespondNoContent writes a 204 No Content status code to the context.
func RespondNoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
