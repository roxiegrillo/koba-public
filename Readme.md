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

## The description

The signature still defaults the verb and the status. What it cannot say, the
API states as data, implementing `APIDocumentation`:

```go
func (*Polls) Routes() map[string]string {
	return map[string]string{
		"ReadLog": "/polls/:poll_id/log",
	}
}

func (*Polls) APIDocumentation() koba.APIDocumentation {
	return koba.APIDocumentation{
		Operations: map[string]koba.OperationDocumentation{
			"ReadLog": {
				Method:        http.MethodGet,
				SuccessStatus: http.StatusOK,
				Parameters: []koba.ParameterDocumentation{
					koba.ParameterOf[int]("page", koba.ParameterInQuery,
						"The page to read.", false),
				},
			},
		},
	}
}
```

`ReadLog` is now a `GET` with a path value and a query parameter: the route
names `poll_id`, and the description declares the verb, the success status
and each parameter. What no description states, the conventions above
provide.

## The plan

`AddApi` no longer discovers endpoints at serving time. It compiles the
description once into a plan: for each operation, an invoker that receives
the request context and a way to decode a body, and knows nothing of routing.
The router consumes the plan; the walk over the value that used to be the
serving became a build step that produces a value.

Built with the `koba_static` tag, the same plan is served from generated
direct calls instead of reflection; both answer through one plan.

## The contract

The OpenAPI document is generated from the same description: route, verb,
status and parameters are written once, and the server and the document read
the same fact. `openapi.Mount` serves the JSON contract and a self-contained
Swagger UI on the app's router.

The contract is also a value of its own. An API that implements
`WithAPIContract` declares the executable boundary beside the functions:
where each operation's request comes from, what it must satisfy, and who may
call it.

```go
type castRequestContract struct {
	PollID koba.Binding[string]
	Ballot koba.BodyObjectBinding[ballotContract]
}

func (*Polls) APIContract() koba.APIContract {
	return koba.APIContract{Operations: map[string]koba.OperationContract{
		"Cast": {
			Request: castRequestContract{
				PollID: koba.Path[string]("poll_id", koba.MinLength(1)),
				Ballot: koba.BodyObject(ballotContract{
					Nullifier: koba.Value[string](koba.Required(), koba.MinLength(1)),
				}, koba.Required()),
			},
		},
	}}
}
```

A binding is a typed leaf that names where a value comes from — `Path`,
`Query`, `Header`, `Cookie`, a value inside the body, or the whole body —
and its input carries the rules: `Required`, `Default`, `Min`, `Max`,
lengths, `OneOf`, `Matches`. The rules are checked against themselves when
the API is described: a minimum on a string, a default that violates its own
constraint, a pattern that does not compile, are refused at registration,
not at the first request.

At serving time the request is built, not parsed. The compiled bindings are
the only bridge between HTTP values and the request fields; each value is
validated against its contract, and a violation answers `400` naming the
field and the reason — before the function runs. The same input publishes
its constraint to the OpenAPI document: the check the runtime enforces and
the document the world reads are one fact with two readers.

An operation may declare the scopes a caller must hold. The scopes are
checked before the request is constructed, so an unauthorized caller learns
nothing about the shape of the input; an operation that declares scopes when
no authentication middleware is configured is refused when the API is added.

## The artifacts

A `kobaapp.Definition` gathers the APIs of a service, its OpenAPI output and
its static targets in one value. Its command writes the generated plan, the
OpenAPI JSON and a lock manifest with their digests; `--check` verifies the
committed artifacts byte for byte.

## The router

The endpoints are served by `internal/httprouter`, a small router over
`net/http`'s `ServeMux`: method-aware patterns, path values, a middleware
chain and a request context. An app exposes it through `WithRouter` for the
rare need this module does not cover.
