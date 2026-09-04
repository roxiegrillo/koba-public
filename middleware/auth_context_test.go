package middleware

import (
	"context"
	"testing"
)

func TestPrincipalContextCopiesPermissions(t *testing.T) {
	permissions := []string{"items:read"}
	ctx := WithPrincipal(context.Background(), Principal{UserID: "user-1", Permissions: permissions})
	permissions[0] = "mutated"
	principal, ok := PrincipalFromContext(ctx)
	if !ok || principal.UserID != "user-1" || principal.Permissions[0] != "items:read" {
		t.Fatalf("PrincipalFromContext() = %#v, %v", principal, ok)
	}
	principal.Permissions[0] = "mutated-again"
	again, _ := PrincipalFromContext(ctx)
	if again.Permissions[0] != "items:read" {
		t.Fatalf("stored permissions were mutated: %#v", again.Permissions)
	}
}
