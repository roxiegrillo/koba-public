package authorization

import (
	"context"
	"errors"
	"reflect"
	"testing"

	appmiddleware "github.com/roxiegrillo/koba/v2/middleware"
)

func TestAuthorizeEvaluatesPrincipalAndScopes(t *testing.T) {
	authenticated := appmiddleware.WithPrincipal(context.Background(), appmiddleware.Principal{
		UserID: " user-1 ",
		Scope:  "unrelated read:items write:items",
	})

	if err := Authorize(context.Background(), false, []string{"read:items"}); err != nil {
		t.Fatalf("public authorization returned %v", err)
	}
	if err := Authorize(context.Background(), true, nil); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("missing principal error = %v", err)
	}
	if err := Authorize(authenticated, true, []string{"read:items", "write:items"}); err != nil {
		t.Fatalf("complete scopes returned %v", err)
	}

	err := Authorize(authenticated, true, []string{"read:items", "delete:items"})
	var insufficient *InsufficientScopeError
	if !errors.As(err, &insufficient) || !reflect.DeepEqual(insufficient.Required, []string{"read:items", "delete:items"}) {
		t.Fatalf("insufficient scope error = %#v", err)
	}
}

func TestSubjectRequiresNonBlankAuthenticatedPrincipal(t *testing.T) {
	blank := appmiddleware.WithPrincipal(context.Background(), appmiddleware.Principal{UserID: " "})
	if _, err := Subject(blank); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("blank subject error = %v", err)
	}

	authenticated := appmiddleware.WithPrincipal(context.Background(), appmiddleware.Principal{UserID: " user-1 "})
	if subject, err := Subject(authenticated); err != nil || subject != "user-1" {
		t.Fatalf("Subject() = %q, %v", subject, err)
	}
}
