package httprouter

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"syscall"
	"time"
)

// DefaultWriter is where Logger writes.
var DefaultWriter io.Writer = os.Stdout

// DefaultErrorWriter is where Recovery writes the panics it recovers.
var DefaultErrorWriter io.Writer = os.Stderr

// Logger returns middleware that writes one line per request to
// DefaultWriter, after the chain has run: time, status, duration, method and
// path.
func Logger() HandlerFunc {
	return func(context *Context) {
		started := time.Now()
		requestPath := context.Request.URL.Path
		if query := context.Request.URL.RawQuery; query != "" {
			requestPath += "?" + query
		}
		context.Next()
		fmt.Fprintf(
			DefaultWriter,
			"[httprouter] %s | %3d | %13v | %-7s %s\n",
			time.Now().Format("2006/01/02 - 15:04:05"),
			context.Writer.Status(),
			time.Since(started),
			context.Request.Method,
			requestPath,
		)
	}
}

// Recovery returns middleware that recovers a panic further down the chain,
// writes it with its stack to DefaultErrorWriter and answers 500 when the
// response has not started. A panic caused by the client's closed connection
// is not answered.
func Recovery() HandlerFunc {
	return func(context *Context) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if isBrokenConnection(recovered) {
				context.Abort()
				return
			}
			fmt.Fprintf(
				DefaultErrorWriter,
				"[httprouter] %s panic recovered: %v\n%s\n",
				time.Now().Format("2006/01/02 - 15:04:05"),
				recovered,
				debug.Stack(),
			)
			context.AbortWithStatus(http.StatusInternalServerError)
		}()
		context.Next()
	}
}

func isBrokenConnection(recovered any) bool {
	err, ok := recovered.(error)
	if !ok {
		return false
	}
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET)
}
