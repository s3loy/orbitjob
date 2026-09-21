package http

import (
	"bytes"
	"io"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"

	"orbitjob/internal/admin/http/apperror"
)

// maxAdminRequestBody caps a request body so a single call cannot make the
// server allocate without bound. Every admin endpoint takes a JSON document
// that is a handful of kilobytes at most; 1 MiB leaves room for a large policy
// document while staying far below anything that would threaten the process.
const maxAdminRequestBody = 1 << 20

// limitRequestBody buffers a bounded request body and hands the handler a
// re-readable copy. Reading past the cap is rejected with 413 rather than
// truncated, because a truncated body would bind as a valid partial document.
func limitRequestBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil {
			c.Next()
			return
		}

		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxAdminRequestBody+1))
		if err != nil {
			apperror.Write(c, stdhttp.StatusBadRequest, apperror.APIError{
				Code:    apperror.CodeMalformedRequest,
				Message: "request body could not be read",
			})
			c.Abort()
			return
		}
		if len(body) > maxAdminRequestBody {
			apperror.Write(c, stdhttp.StatusRequestEntityTooLarge, apperror.APIError{
				Code:    apperror.CodeMalformedRequest,
				Message: "request body exceeds the size limit",
			})
			c.Abort()
			return
		}

		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Next()
	}
}
