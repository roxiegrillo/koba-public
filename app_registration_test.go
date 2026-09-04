package koba

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"

	httprouter "github.com/roxiegrillo/koba/v2/internal/httprouter"
	appmiddleware "github.com/roxiegrillo/koba/v2/middleware"
)

type registrationFixtureAPI struct{}

func (*registrationFixtureAPI) First() map[string]string {
	return map[string]string{"operation": "first"}
}

type scopedRegistrationAPI struct {
	invocations int
}

func (api *scopedRegistrationAPI) Protected() map[string]string {
	api.invocations++
	return map[string]string{"operation": "protected"}
}

func (*scopedRegistrationAPI) Public() map[string]string {
	return map[string]string{"operation": "public"}
}

func (*scopedRegistrationAPI) Routes() map[string]string {
	return map[string]string{
		"Protected": "/protected",
		"Public":    "/public",
	}
}

func (*scopedRegistrationAPI) APIContract() APIContract {
	return APIContract{Operations: map[string]OperationContract{
		"Protected": {
			Authorization: AuthorizationContract{
				RequiredScopes: []string{"read:protected"},
			},
		},
	}}
}

func (*registrationFixtureAPI) Second() map[string]string {
	return map[string]string{"operation": "second"}
}

func (*registrationFixtureAPI) Routes() map[string]string {
	return map[string]string{
		"First":  "/first",
		"Second": "/second",
	}
}

func TestRegisteredAPIDescriptionsReturnsDefensiveSlice(t *testing.T) {
	app := MakeRawApp()
	app.AddApi(&registrationFixtureAPI{})

	described := app.(WithAPIDescriptions).RegisteredAPIDescriptions()
	if len(described) != 1 {
		t.Fatalf("description count = %d, want 1", len(described))
	}
	described[0] = APIDescription{}

	again := app.(WithAPIDescriptions).RegisteredAPIDescriptions()
	if len(again) != 1 || len(again[0].Operations) != 2 {
		t.Fatalf("mutating returned descriptions changed app state: %#v", again)
	}
}

func TestUseGroupAppliesMatchingMiddlewareInRegistrationOrder(t *testing.T) {
	app := MakeRawApp()
	grouped := regexp.MustCompile(`^/first$`)
	app.(WithMiddlewareRegexUse).UseGroup(
		grouped,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Add("X-Middleware", "one")
		}),
	)
	app.(WithMiddlewareRegexUse).UseGroup(
		grouped,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Add("X-Middleware", "two")
		}),
	)
	app.AddApi(&registrationFixtureAPI{})
	router := app.(WithRouter).Router()

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/first", nil))
	if got, want := first.Header().Values("X-Middleware"), []string{"one", "two"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("matching middleware headers = %q, want %q", got, want)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/second", nil))
	if got := second.Header().Values("X-Middleware"); len(got) != 0 {
		t.Fatalf("non-matching middleware headers = %q, want none", got)
	}
}

func TestAddAPIFailsClosedWhenScopedOperationHasNoAuthenticationMiddleware(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil || !strings.Contains(fmt.Sprint(recovered), "no authentication middleware is configured") {
			t.Fatalf("AddApi() panic = %#v, want missing authentication middleware", recovered)
		}
	}()
	MakeRawApp().AddApi(&scopedRegistrationAPI{})
}

func TestAuthenticationOptionAppliesOnlyToScopedOperations(t *testing.T) {
	authenticationCalls := 0
	authentication := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		authenticationCalls++
		ctx := appmiddleware.WithPrincipal(request.Context(), appmiddleware.Principal{
			UserID: "user-1",
			Scope:  "read:protected",
		})
		*request = *request.WithContext(ctx)
	})
	api := &scopedRegistrationAPI{}
	app := MakeRawAppWithOptions(WithAuthenticationMiddleware(authentication))
	groupSawPrincipal := false
	app.(WithMiddlewareRegexUse).UseGroup(
		regexp.MustCompile(`^/protected$`),
		http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			_, groupSawPrincipal = appmiddleware.PrincipalFromContext(request.Context())
		}),
	)
	app.AddApi(api)
	router := app.(WithRouter).Router()

	public := httptest.NewRecorder()
	router.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/public", nil))
	if public.Code != http.StatusOK || authenticationCalls != 0 {
		t.Fatalf("public response/calls = %d/%d, want 200/0", public.Code, authenticationCalls)
	}

	protected := httptest.NewRecorder()
	router.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "/protected", nil))
	if protected.Code != http.StatusOK || authenticationCalls != 1 || api.invocations != 1 || !groupSawPrincipal {
		t.Fatalf("protected response/auth/API calls/principal = %d/%d/%d/%v, want 200/1/1/true", protected.Code, authenticationCalls, api.invocations, groupSawPrincipal)
	}
}

func TestAuthorizeOperationRequiresAllScopesWithoutUsingPermissions(t *testing.T) {
	tests := []struct {
		name       string
		principal  *appmiddleware.Principal
		wantStatus int
	}{
		{name: "missing principal", wantStatus: http.StatusUnauthorized},
		{
			name:       "missing one required scope",
			principal:  &appmiddleware.Principal{UserID: "user-1", Scope: "read:protected"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "permission does not satisfy scope",
			principal:  &appmiddleware.Principal{UserID: "user-1", Scope: "read:protected", Permissions: []string{"write:protected"}},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "scope matching is case sensitive",
			principal:  &appmiddleware.Principal{UserID: "user-1", Scope: "read:protected Write:protected"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "only ASCII spaces delimit scopes",
			principal:  &appmiddleware.Principal{UserID: "user-1", Scope: "read:protected\twrite:protected"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "all exact scopes",
			principal:  &appmiddleware.Principal{UserID: "user-1", Scope: "unrelated read:protected write:protected"},
			wantStatus: http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invoked := false
			handler := makeOperationHandler(OperationDescription{
				MethodName: "Protected",
				Route:      "/protected",
				HTTPMethod: http.MethodGet,
				OutputType: reflect.TypeFor[map[string]bool](),
				Documentation: OperationDocumentation{
					RequiredScopes: []string{"read:protected", "write:protected"},
				},
			}, func(Invocation) (InvocationResult, error) {
				invoked = true
				return InvocationResult{Value: map[string]bool{"ok": true}, HasValue: true}, nil
			})
			app := MakeRawApp().(WithRouter).Router()
			app.GET("/protected", func(c *httprouter.Context) {
				if test.principal != nil {
					c.Request = c.Request.WithContext(appmiddleware.WithPrincipal(context.Background(), *test.principal))
				}
				handler(c)
			})
			response := httptest.NewRecorder()
			app.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/protected", nil))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if invoked != (test.wantStatus == http.StatusOK) {
				t.Fatalf("invoked = %v for status %d", invoked, test.wantStatus)
			}
			if test.wantStatus == http.StatusForbidden && !strings.Contains(response.Header().Get("WWW-Authenticate"), "insufficient_scope") {
				t.Fatalf("WWW-Authenticate = %q", response.Header().Get("WWW-Authenticate"))
			}
		})
	}
}
