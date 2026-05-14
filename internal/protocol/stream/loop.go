package stream

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// StreamLoop is a drop-in replacement for c.Stream() that does NOT finalize the
// HTTP response writer when it returns.
//
// MENTION: c.Stream() closes the HTTP response writer on exit, so any writes after
// it (error events, final chunks, usage stats) are silently dropped. Use StreamLoop
// whenever the caller needs to write to the response after the loop ends.
//
// Returns true if the client disconnected mid-stream (mirrors c.Stream behavior).
func StreamLoop(c *gin.Context, step func(w io.Writer) bool) bool {
	w := c.Writer
	flusher, ok := w.(http.Flusher)
	if !ok {
		return false
	}
	clientGone := w.CloseNotify()
	for {
		select {
		case <-clientGone:
			return true
		default:
			if !step(w) {
				flusher.Flush()
				return false
			}
			flusher.Flush()
		}
	}
}
