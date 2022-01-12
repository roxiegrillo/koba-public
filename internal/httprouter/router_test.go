package httprouter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, handler http.Handler, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, target, body))
	return response
}

func TestRoutesMatchByMethodAndPattern(t *testing.T) {
	engine := New()
	engine.GET("/items/{id}", func(c *Context) { c.String(http.StatusOK, "get %s", c.Param("id")) })
	engine.POST("/items", func(c *Context) { c.Status(http.StatusCreated) })
	engine.PUT("/items/:id", func(c *Context) { c.String(http.StatusOK, "put %s", c.Params.ByName("id")) })
	engine.PATCH("/items/{id}", func(c *Context) { c.String(http.StatusOK, "patch") })
	engine.DELETE("/items/{id}", func(c *Context) { c.Status(http.StatusNoContent) })
	engine.GET("/items/special", func(c *Context) { c.String(http.StatusOK, "special") })
	engine.GET("/files/*rest", func(c *Context) { c.String(http.StatusOK, "file %s", c.Param("rest")) })
	engine.GET("/", func(c *Context) { c.String(http.StatusOK, "root") })

	for _, test := range []struct {
		method, target string
		wantStatus     int
		wantBody       string
	}{
		{http.MethodGet, "/items/item-1", http.StatusOK, "get item-1"},
		{http.MethodPost, "/items", http.StatusCreated, ""},
		{http.MethodPut, "/items/item-2", http.StatusOK, "put item-2"},
		{http.MethodPatch, "/items/item-3", http.StatusOK, "patch"},
		{http.MethodDelete, "/items/item-4", http.StatusNoContent, ""},
		{http.MethodGet, "/items/special", http.StatusOK, "special"},
		{http.MethodGet, "/files/a/b/c.txt", http.StatusOK, "file a/b/c.txt"},
		{http.MethodGet, "/files/", http.StatusOK, "file "},
		{http.MethodGet, "/", http.StatusOK, "root"},
		{http.MethodGet, "/missing", http.StatusNotFound, "404 page not found"},
		{http.MethodDelete, "/items", http.StatusNotFound, "404 page not found"},
	} {
		response := serve(t, engine, test.method, test.target, nil)
		if response.Code != test.wantStatus || response.Body.String() != test.wantBody {
			t.Errorf("%s %s = %d %q, want %d %q", test.method, test.target, response.Code, response.Body.String(), test.wantStatus, test.wantBody)
		}
	}
}

func TestPatternsAreNormalized(t *testing.T) {
	for input, want := range map[string]string{
		"/items/:id":       "/items/{id}",
		"/items/{id}/sub":  "/items/{id}/sub",
		"/docs/*asset":     "/docs/{asset...}",
		"/docs/{asset...}": "/docs/{asset...}",
		"/":                "/{$}",
		"/items/":          "/items/{$}",
		"/a/:x/b/:y":       "/a/{x}/b/{y}",
		"/exact/{$}":       "/exact/{$}",
	} {
		if got := toPattern(input).text; got != want {
			t.Errorf("toPattern(%q) = %q, want %q", input, got, want)
		}
	}
	if got := toPattern("/a/:x/b/*y").names; strings.Join(got, ",") != "x,y" {
		t.Errorf("names = %v, want [x y]", got)
	}
}

func TestGroupsJoinPrefixesAndPrependMiddleware(t *testing.T) {
	engine := New()
	var order []string
	engine.Use(func(c *Context) { order = append(order, "engine") })
	service := engine.Group("/service", func(c *Context) { order = append(order, "service") })
	if got := service.BasePath(); got != "/service" {
		t.Fatalf("BasePath() = %q, want /service", got)
	}
	items := service.Group("/items/:id")
	items.Use(func(c *Context) { order = append(order, "items") })
	items.GET("", func(c *Context) {
		order = append(order, "handler")
		c.String(http.StatusOK, c.FullPath()+" "+c.Param("id"))
	})
	engine.Use(func(c *Context) { order = append(order, "late") })

	response := serve(t, engine, http.MethodGet, "/service/items/item-1", nil)
	if got, want := response.Body.String(), "/service/items/{id} item-1"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if got, want := strings.Join(order, ">"), "engine>service>items>handler"; got != want {
		t.Fatalf("order = %q, want %q", got, want)
	}
}

func TestNextAndAbortControlTheChain(t *testing.T) {
	engine := New()
	var order []string
	engine.Use(func(c *Context) {
		order = append(order, "before")
		c.Next()
		order = append(order, "after:"+http.StatusText(c.Writer.Status()))
	})
	engine.Use(func(c *Context) { order = append(order, "returns without Next") })
	engine.GET("/abort", func(c *Context) {
		order = append(order, "abort")
		c.AbortWithStatusJSON(http.StatusForbidden, H{"error": "forbidden"})
	}, func(c *Context) { order = append(order, "never") })
	engine.GET("/ok", func(c *Context) { order = append(order, "ok"); c.Status(http.StatusAccepted) })

	response := serve(t, engine, http.MethodGet, "/abort", nil)
	if response.Code != http.StatusForbidden || response.Body.String() != `{"error":"forbidden"}` {
		t.Fatalf("abort response = %d %q", response.Code, response.Body.String())
	}
	if got, want := strings.Join(order, ">"), "before>returns without Next>abort>after:Forbidden"; got != want {
		t.Fatalf("order = %q, want %q", got, want)
	}
	order = nil
	response = serve(t, engine, http.MethodGet, "/ok", nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", response.Code)
	}
	if got, want := strings.Join(order, ">"), "before>returns without Next>ok>after:Accepted"; got != want {
		t.Fatalf("order = %q, want %q", got, want)
	}
}

func TestUnmatchedRequestsRunTheEngineMiddlewareWithoutAFullPath(t *testing.T) {
	engine := New()
	var seen []string
	engine.Use(func(c *Context) {
		seen = append(seen, "path="+c.FullPath())
		if c.Request.URL.Path == "/probe" {
			c.String(http.StatusOK, "probe")
			c.Abort()
		}
	})
	engine.NoRoute(func(c *Context) { c.String(http.StatusGone, "gone") })
	engine.GET("/items", func(c *Context) { c.Status(http.StatusNoContent) })

	if response := serve(t, engine, http.MethodGet, "/probe", nil); response.Code != http.StatusOK || response.Body.String() != "probe" {
		t.Fatalf("probe = %d %q", response.Code, response.Body.String())
	}
	if response := serve(t, engine, http.MethodGet, "/other", nil); response.Code != http.StatusGone || response.Body.String() != "gone" {
		t.Fatalf("no-route handler = %d %q", response.Code, response.Body.String())
	}
	if response := serve(t, engine, http.MethodPost, "/items", nil); response.Code != http.StatusGone {
		t.Fatalf("known path under another method = %d, want the no-route chain", response.Code)
	}
	if got, want := strings.Join(seen, ","), "path=,path=,path="; got != want {
		t.Fatalf("full paths = %q, want %q", got, want)
	}
	plain := New()
	plain.Use(func(c *Context) { c.Status(http.StatusTeapot) })
	if response := serve(t, plain, http.MethodGet, "/nowhere", nil); response.Code != http.StatusTeapot || response.Body.Len() != 0 {
		t.Fatalf("status set by middleware = %d %q, want 418 and no body", response.Code, response.Body.String())
	}
}

func TestCustomMethodsAndHeadersAndQuery(t *testing.T) {
	engine := New()
	engine.Handle("BREW", "/coffee", func(c *Context) {
		c.Header("X-Kind", c.Query("kind"))
		c.Header("X-Removed", "value")
		c.Header("X-Removed", "")
		c.String(http.StatusOK, c.GetHeader("X-Cup"))
	})
	response := serve(t, engine, "BREW", "/coffee?kind=espresso", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get("X-Kind"); got != "espresso" {
		t.Fatalf("X-Kind = %q", got)
	}
	if _, present := response.Header()["X-Removed"]; present {
		t.Fatal("empty Header value did not remove the header")
	}
	request := httptest.NewRequest("BREW", "/coffee", nil)
	request.Header.Set("X-Cup", "large")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Body.String() != "large" {
		t.Fatalf("body = %q, want large", response.Body.String())
	}
}

func TestResponseWriterRecordsTheStatusUntilTheBodyStarts(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := CreateTestContext(recorder)
	if c.Writer.Status() != http.StatusOK || c.Writer.Size() != -1 || c.Writer.Written() {
		t.Fatalf("fresh writer = %d/%d/%v", c.Writer.Status(), c.Writer.Size(), c.Writer.Written())
	}
	c.Status(http.StatusAccepted)
	if recorder.Code != http.StatusOK || c.Writer.Written() {
		t.Fatal("Status sent the headers early")
	}
	if _, err := c.Writer.WriteString("ab"); err != nil {
		t.Fatal(err)
	}
	c.Writer.WriteHeader(http.StatusTeapot)
	if recorder.Code != http.StatusAccepted || c.Writer.Status() != http.StatusAccepted || c.Writer.Size() != 2 {
		t.Fatalf("after write = %d/%d/%d", recorder.Code, c.Writer.Status(), c.Writer.Size())
	}
	c.Writer.Flush()
	if _, _, err := c.Writer.Hijack(); err == nil {
		t.Fatal("Hijack succeeded on a recorder")
	}
	if http.NewResponseController(c.Writer).Flush() != nil {
		t.Fatal("ResponseController cannot reach the recorder")
	}
}

func TestDataJSONStringAndRedirect(t *testing.T) {
	engine := New()
	engine.GET("/json", func(c *Context) { c.JSON(http.StatusCreated, H{"b": 1, "a": "x"}) })
	engine.GET("/typed", func(c *Context) {
		c.Header("Content-Type", "application/problem+json")
		c.JSON(http.StatusBadRequest, H{"error": true})
	})
	engine.GET("/empty", func(c *Context) { c.JSON(http.StatusNoContent, H{"ignored": true}) })
	engine.GET("/data", func(c *Context) { c.Data(http.StatusOK, "text/html", []byte("<b>hi</b>")) })
	engine.GET("/text", func(c *Context) { c.String(http.StatusOK, "100%% %d", 5) })
	engine.GET("/raw", func(c *Context) { c.String(http.StatusOK, "100%") })
	engine.GET("/docs", func(c *Context) { c.Redirect(http.StatusPermanentRedirect, "/docs/") })

	for _, test := range []struct {
		target, wantType, wantBody string
		wantStatus                 int
	}{
		{"/json", "application/json; charset=utf-8", `{"a":"x","b":1}`, http.StatusCreated},
		{"/typed", "application/problem+json", `{"error":true}`, http.StatusBadRequest},
		{"/empty", "application/json; charset=utf-8", "", http.StatusNoContent},
		{"/data", "text/html", "<b>hi</b>", http.StatusOK},
		{"/text", "text/plain; charset=utf-8", "100% 5", http.StatusOK},
		{"/raw", "text/plain; charset=utf-8", "100%", http.StatusOK},
	} {
		response := serve(t, engine, http.MethodGet, test.target, nil)
		if response.Code != test.wantStatus || response.Body.String() != test.wantBody || response.Header().Get("Content-Type") != test.wantType {
			t.Errorf("%s = %d %q %q, want %d %q %q", test.target, response.Code, response.Body.String(), response.Header().Get("Content-Type"), test.wantStatus, test.wantBody, test.wantType)
		}
	}
	response := serve(t, engine, http.MethodGet, "/docs", nil)
	if response.Code != http.StatusPermanentRedirect || response.Header().Get("Location") != "/docs/" {
		t.Fatalf("redirect = %d %q", response.Code, response.Header().Get("Location"))
	}
	defer func() {
		if recover() == nil {
			t.Fatal("Redirect accepted a non-redirect status")
		}
	}()
	c, _ := CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Redirect(http.StatusOK, "/")
}

type payload struct {
	Name string `json:"name"`
}

func TestBindingDecodesJSONAndCachesTheBody(t *testing.T) {
	c, _ := CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"one"}`))
	var first, second payload
	if err := c.ShouldBindBodyWith(&first, BindingJSON); err != nil || first.Name != "one" {
		t.Fatalf("first decode = %v %+v", err, first)
	}
	cached, _ := c.Get(BodyBytesKey)
	if !bytes.Equal(cached.([]byte), []byte(`{"name":"one"}`)) {
		t.Fatalf("cached body = %q", cached)
	}
	if err := c.ShouldBindBodyWith(&second, BindingJSON); err != nil || second.Name != "one" {
		t.Fatalf("second decode = %v %+v", err, second)
	}
	if err := c.ShouldBindBodyWith(&second, BindingKind(99)); err == nil {
		t.Fatal("unknown binding kind accepted")
	}

	c, _ = CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	if err := c.ShouldBindBodyWith(&first, BindingJSON); !errors.Is(err, io.EOF) {
		t.Fatalf("empty body error = %v, want io.EOF", err)
	}

	c, _ = CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"two"}`))
	if err := c.ShouldBindJSON(&first); err != nil || first.Name != "two" {
		t.Fatalf("ShouldBindJSON = %v %+v", err, first)
	}
	c, _ = CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":`))
	if err := c.BindJSON(&first); err == nil || c.Writer.Status() != http.StatusBadRequest || !c.IsAborted() {
		t.Fatalf("BindJSON on malformed body = %v, status %d, aborted %v", err, c.Writer.Status(), c.IsAborted())
	}
	if c.Writer.Written() {
		t.Fatal("BindJSON sent the headers")
	}
	c, _ = CreateTestContext(httptest.NewRecorder())
	if err := c.ShouldBindJSON(&first); err == nil {
		t.Fatal("a context without a request decoded a body")
	}
}

func TestKeysAndContextDelegation(t *testing.T) {
	c, _ := CreateTestContext(httptest.NewRecorder())
	if c.Err() != nil || c.Done() != nil || c.Value("missing") != nil {
		t.Fatal("a context without a request is not a background context")
	}
	if _, ok := c.Deadline(); ok {
		t.Fatal("a context without a request has a deadline")
	}
	c.Set("user", "ana")
	if value, exists := c.Get("user"); !exists || value != "ana" || c.MustGet("user") != "ana" || c.Value("user") != "ana" {
		t.Fatal("stored value not returned")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustGet did not panic")
		}
	}()
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "request"))
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	if c.Value(key{}) != "request" {
		t.Fatal("request context value not reached")
	}
	cancel()
	if !errors.Is(c.Err(), context.Canceled) {
		t.Fatal("cancellation not reached")
	}
	<-c.Done()
	c.MustGet("absent")
}

func TestRecoveryAnswersPanicsAndLoggerWritesALine(t *testing.T) {
	var logs, errs bytes.Buffer
	DefaultWriter, DefaultErrorWriter = &logs, &errs
	defer func() { DefaultWriter, DefaultErrorWriter = io.Discard, io.Discard }()
	engine := Default()
	engine.GET("/panic", func(c *Context) { panic("boom") })
	engine.GET("/fine", func(c *Context) { c.Status(http.StatusNoContent) })
	engine.GET("/pipe", func(c *Context) { panic(errors.New("write: broken pipe")) })

	if response := serve(t, engine, http.MethodGet, "/panic", nil); response.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d", response.Code)
	}
	if !strings.Contains(errs.String(), "boom") || !strings.Contains(errs.String(), "router_test.go") {
		t.Fatalf("panic log = %q", errs.String())
	}
	if response := serve(t, engine, http.MethodGet, "/fine?x=1", nil); response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
	if !strings.Contains(logs.String(), "| 500 |") || !strings.Contains(logs.String(), "GET     /fine?x=1") {
		t.Fatalf("log lines = %q", logs.String())
	}
}

func TestWrapHAndStandardHandlers(t *testing.T) {
	engine := New()
	mux := http.NewServeMux()
	mux.HandleFunc("/wrapped/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, r.URL.Path)
	})
	after := false
	engine.GET("/wrapped/{rest...}", WrapH(mux), func(c *Context) {
		after = c.Writer.Status() == http.StatusAccepted && c.Writer.Size() == len("/wrapped/x")
	})
	response := serve(t, engine, http.MethodGet, "/wrapped/x", nil)
	if response.Code != http.StatusAccepted || response.Body.String() != "/wrapped/x" || !after {
		t.Fatalf("wrapped = %d %q, after=%v", response.Code, response.Body.String(), after)
	}
}

func TestRegistrationRefusesWhatItCannotRoute(t *testing.T) {
	for name, register := range map[string]func(*Engine){
		"no method":  func(e *Engine) { e.Handle("", "/x", func(*Context) {}) },
		"no handler": func(e *Engine) { e.GET("/x") },
		"conflict": func(e *Engine) {
			e.GET("/a/{x}/c", func(*Context) {})
			e.GET("/a/b/{y}", func(*Context) {})
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("registration did not panic")
				}
			}()
			register(New())
		})
	}
}

func TestRunRefusesABusyAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer listener.Close()
	if err := New().Run(listener.Addr().String()); err == nil {
		t.Fatal("Run bound an address in use")
	}
}
