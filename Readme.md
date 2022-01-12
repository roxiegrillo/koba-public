# koba

A service is a set of functions. This module serves them over HTTP without
asking the functions to know it: an API is an ordinary Go value, and every
exported method of that value is an endpoint.

```go
type Polls struct{}

func (*Polls) ListPolls() []Poll { ... }

func main() {
	app := koba.MakeRawApp()
	app.AddApi(&Polls{})
	log.Fatal(app.Run(":8080"))
}
```

`ListPolls` is served at `GET /list-polls` and answers with the JSON
encoding of what it returns. A method that takes a value is served over
`POST`, and the value is decoded from the JSON body:

```go
func (*Polls) Cast(request CastRequest) Receipt { ... }
```

## Conventions

* Every exported method of a registered value is an endpoint. A method takes
  at most one parameter, the request, and returns at most one value, the
  response; any other shape cannot be served, and registering it is an error.
* The HTTP method follows the signature: a method that takes a request is
  served over `POST`, and the request is decoded from the JSON body; a method
  that takes nothing is served over `GET`.
* The route is the method's name in lower case, with a dash between words:
  `DeclarePoll` is served at `/declare-poll`. An API that implements
  `WithRoutesAPI` chooses the routes of the methods it names; `Routes` itself
  is not an endpoint.
* A method that returns a value answers `200 OK` with the value encoded as
  JSON; a method that returns nothing answers `204 No Content`.
* A body that does not decode into the request answers `400 Bad Request`
  with the same kind of JSON body as a failure.
* A method that panics answers `500 Internal Server Error` with a JSON body
  that carries the message: `{"status": 500, "error":
  "internal_server_error", "message": "..."}`.

## The router

The endpoints are served by `internal/httprouter`, a small router over
`net/http`'s `ServeMux`: method-aware patterns, path values, a middleware
chain and a request context. An app exposes it through `WithRouter` for the
rare need this module does not cover.
