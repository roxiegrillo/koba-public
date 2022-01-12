package httprouter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"sync"
	"time"
)

// BodyBytesKey is the context key under which ShouldBindBodyWith caches the
// request body, so that the body can be decoded more than once.
const BodyBytesKey = "_httprouter/body-bytes"

// BindingKind names how a request body is decoded.
type BindingKind int

// BindingJSON decodes the body as a JSON document.
const BindingJSON BindingKind = iota

// abortIndex is the chain position past every handler.
const abortIndex int8 = math.MaxInt8 >> 1

// Param is one path value, named by the route's pattern.
type Param struct {
	Key   string
	Value string
}

// Params is the path values of a matched route, in pattern order.
type Params []Param

// Get returns the value named key and whether the route names it.
func (params Params) Get(key string) (string, bool) {
	for _, param := range params {
		if param.Key == key {
			return param.Value, true
		}
	}
	return "", false
}

// ByName returns the value named key, or "" when the route does not name it.
func (params Params) ByName(key string) string {
	value, _ := params.Get(key)
	return value
}

// ResponseWriter is the response as the chain sees it: a status that is
// recorded when set and sent with the first byte of the body, or when the
// chain ends, and a count of the bytes written.
type ResponseWriter interface {
	http.ResponseWriter
	http.Flusher
	http.Hijacker
	// Status is the status sent, or the one that will be.
	Status() int
	// Size is the number of body bytes written, -1 before the first write.
	Size() int
	// Written reports whether the status has been sent.
	Written() bool
	// WriteHeaderNow sends the status, when it has not been sent yet.
	WriteHeaderNow()
	// WriteString writes s to the body.
	WriteString(s string) (int, error)
}

// Context carries one request through its chain: the request, the response
// writer, the path values, and the values handlers share through Set and Get.
// It is a context.Context that delegates to the request's.
type Context struct {
	Request *http.Request
	Writer  ResponseWriter
	Params  Params

	engine   *Engine
	writer   *responseWriter
	handlers []HandlerFunc
	index    int8
	fullPath string

	mu   sync.RWMutex
	keys map[string]any
}

func newContext(engine *Engine, writer http.ResponseWriter, request *http.Request) *Context {
	context := &Context{engine: engine, Request: request, index: -1}
	context.writer = &responseWriter{ResponseWriter: writer, status: http.StatusOK, size: notWritten, context: context}
	context.Writer = context.writer
	return context
}

func contextOf(writer http.ResponseWriter) (*Context, bool) {
	recorded, ok := writer.(*responseWriter)
	if !ok {
		return nil, false
	}
	return recorded.context, true
}

// serveNoRoute runs the engine's middleware and NoRoute handlers for a
// request no route matches. The status starts at 404; when nothing was
// written and the status still is 404, a plain body says so.
func (context *Context) serveNoRoute() {
	context.writer.status = http.StatusNotFound
	engine := context.engine
	context.handlers = make([]HandlerFunc, 0, len(engine.handlers)+len(engine.noRoute))
	context.handlers = append(context.handlers, engine.handlers...)
	context.handlers = append(context.handlers, engine.noRoute...)
	context.Next()
	if context.writer.Written() {
		return
	}
	if context.writer.status == http.StatusNotFound {
		context.writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = context.writer.WriteString("404 page not found")
		return
	}
	context.writer.WriteHeaderNow()
}

// Next runs the rest of the chain. A handler that returns without calling it
// does not stop the chain; Abort does.
func (context *Context) Next() {
	context.index++
	for context.index < int8(len(context.handlers)) {
		context.handlers[context.index](context)
		context.index++
	}
}

// Abort stops the chain: the handlers after the current one do not run.
func (context *Context) Abort() {
	context.index = abortIndex
}

// IsAborted reports whether the chain was aborted.
func (context *Context) IsAborted() bool {
	return context.index >= abortIndex
}

// AbortWithStatus sends the status and aborts the chain.
func (context *Context) AbortWithStatus(code int) {
	context.Status(code)
	context.Writer.WriteHeaderNow()
	context.Abort()
}

// AbortWithStatusJSON writes value as the JSON response with the status and
// aborts the chain.
func (context *Context) AbortWithStatusJSON(code int, value any) {
	context.Abort()
	context.JSON(code, value)
}

// FullPath is the pattern of the matched route, "" when no route matched.
func (context *Context) FullPath() string {
	return context.fullPath
}

// Param is the path value the route names key.
func (context *Context) Param(key string) string {
	return context.Params.ByName(key)
}

// Query is the first value of the query parameter key, "" when absent.
func (context *Context) Query(key string) string {
	return context.Request.URL.Query().Get(key)
}

// GetHeader is the request header key.
func (context *Context) GetHeader(key string) string {
	return context.Request.Header.Get(key)
}

// Header sets the response header key to value, or removes it when value is
// empty.
func (context *Context) Header(key, value string) {
	if value == "" {
		context.Writer.Header().Del(key)
		return
	}
	context.Writer.Header().Set(key, value)
}

// Status records the response status. It is sent with the first byte of the
// body, or when the chain ends.
func (context *Context) Status(code int) {
	context.Writer.WriteHeader(code)
}

// Set stores value under key for the rest of the chain.
func (context *Context) Set(key string, value any) {
	context.mu.Lock()
	defer context.mu.Unlock()
	if context.keys == nil {
		context.keys = make(map[string]any)
	}
	context.keys[key] = value
}

// Get returns the value stored under key and whether one is.
func (context *Context) Get(key string) (any, bool) {
	context.mu.RLock()
	defer context.mu.RUnlock()
	value, exists := context.keys[key]
	return value, exists
}

// MustGet returns the value stored under key and panics when there is none.
func (context *Context) MustGet(key string) any {
	value, exists := context.Get(key)
	if !exists {
		panic("httprouter: no value stored under key " + key)
	}
	return value
}

// JSON writes value, encoded as JSON, as the response body with the status.
// It panics when value cannot be encoded.
func (context *Context) JSON(code int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Errorf("httprouter: encode JSON response: %w", err))
	}
	context.Data(code, "application/json; charset=utf-8", encoded)
}

// String writes the formatted text as the response body with the status.
// With no values, format is written as it is.
func (context *Context) String(code int, format string, values ...any) {
	text := format
	if len(values) > 0 {
		text = sprintf(format, values...)
	}
	context.Data(code, "text/plain; charset=utf-8", []byte(text))
}

// sprintf is called through a variable so that String is not taken for a
// printf wrapper: its format is often a value a handler computes.
var sprintf = fmt.Sprintf

// Data writes data as the response body with the status. The content type is
// set unless the response already has one; a status that allows no body sends
// the headers only.
func (context *Context) Data(code int, contentType string, data []byte) {
	context.Status(code)
	if contentType != "" {
		header := context.Writer.Header()
		if header.Get("Content-Type") == "" {
			header.Set("Content-Type", contentType)
		}
	}
	if !bodyAllowedForStatus(code) {
		context.Writer.WriteHeaderNow()
		return
	}
	_, _ = context.Writer.Write(data)
}

// Redirect sends a redirect to location with the status, which must be a 3xx
// status or 201 Created.
func (context *Context) Redirect(code int, location string) {
	if (code < http.StatusMultipleChoices || code > http.StatusPermanentRedirect) && code != http.StatusCreated {
		panic(fmt.Sprintf("httprouter: cannot redirect with status %d", code))
	}
	http.Redirect(context.Writer, context.Request, location, code)
}

// ShouldBindJSON decodes the request body, a JSON document, into target. An
// empty body yields io.EOF.
func (context *Context) ShouldBindJSON(target any) error {
	if context.Request == nil || context.Request.Body == nil {
		return errors.New("httprouter: the request has no body")
	}
	return decodeJSON(context.Request.Body, target)
}

// BindJSON decodes like ShouldBindJSON; on failure it records a 400 status,
// aborts the chain and returns the error. The handler may still write the
// body of the response.
func (context *Context) BindJSON(target any) error {
	if err := context.ShouldBindJSON(target); err != nil {
		context.Status(http.StatusBadRequest)
		context.Abort()
		return err
	}
	return nil
}

// ShouldBindBodyWith decodes the request body into target the way kind says.
// The body is read once and cached under BodyBytesKey, so the call can be
// repeated with another target.
func (context *Context) ShouldBindBodyWith(target any, kind BindingKind) error {
	var body []byte
	if cached, exists := context.Get(BodyBytesKey); exists {
		if encoded, ok := cached.([]byte); ok {
			body = encoded
		}
	}
	if body == nil {
		if context.Request == nil || context.Request.Body == nil {
			return errors.New("httprouter: the request has no body")
		}
		encoded, err := io.ReadAll(context.Request.Body)
		if err != nil {
			return err
		}
		body = encoded
		context.Set(BodyBytesKey, body)
	}
	switch kind {
	case BindingJSON:
		return decodeJSON(bytes.NewReader(body), target)
	default:
		return fmt.Errorf("httprouter: unknown binding kind %d", kind)
	}
}

// Deadline is the request context's.
func (context *Context) Deadline() (time.Time, bool) {
	if context.Request == nil {
		return time.Time{}, false
	}
	return context.Request.Context().Deadline()
}

// Done is the request context's.
func (context *Context) Done() <-chan struct{} {
	if context.Request == nil {
		return nil
	}
	return context.Request.Context().Done()
}

// Err is the request context's.
func (context *Context) Err() error {
	if context.Request == nil {
		return nil
	}
	return context.Request.Context().Err()
}

// Value returns the value stored under key by Set when key is such a string,
// and the request context's value otherwise.
func (context *Context) Value(key any) any {
	if name, ok := key.(string); ok {
		if value, exists := context.Get(name); exists {
			return value
		}
	}
	if context.Request == nil {
		return nil
	}
	return context.Request.Context().Value(key)
}

func decodeJSON(reader io.Reader, target any) error {
	return json.NewDecoder(reader).Decode(target)
}

// bodyAllowedForStatus reports whether a response with the status may carry a
// body, following RFC 7230 section 3.3.
func bodyAllowedForStatus(status int) bool {
	switch {
	case status >= 100 && status <= 199:
		return false
	case status == http.StatusNoContent, status == http.StatusNotModified:
		return false
	}
	return true
}

const notWritten = -1

// responseWriter records the status until the body starts, and counts the
// bytes written.
type responseWriter struct {
	http.ResponseWriter
	status  int
	size    int
	context *Context
}

var _ ResponseWriter = (*responseWriter)(nil)

func (writer *responseWriter) WriteHeader(code int) {
	if code <= 0 || code == writer.status || writer.Written() {
		return
	}
	writer.status = code
}

func (writer *responseWriter) WriteHeaderNow() {
	if writer.Written() {
		return
	}
	writer.size = 0
	writer.ResponseWriter.WriteHeader(writer.status)
}

func (writer *responseWriter) Write(data []byte) (int, error) {
	writer.WriteHeaderNow()
	count, err := writer.ResponseWriter.Write(data)
	writer.size += count
	return count, err
}

func (writer *responseWriter) WriteString(text string) (int, error) {
	writer.WriteHeaderNow()
	count, err := io.WriteString(writer.ResponseWriter, text)
	writer.size += count
	return count, err
}

func (writer *responseWriter) Status() int {
	return writer.status
}

func (writer *responseWriter) Size() int {
	return writer.size
}

func (writer *responseWriter) Written() bool {
	return writer.size != notWritten
}

// Flush sends the status, when it has not been sent, and flushes the response.
func (writer *responseWriter) Flush() {
	writer.WriteHeaderNow()
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack hands the connection over, when the underlying writer allows it.
func (writer *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("httprouter: the response writer does not support hijacking")
	}
	if writer.size < 0 {
		writer.size = 0
	}
	return hijacker.Hijack()
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (writer *responseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}
