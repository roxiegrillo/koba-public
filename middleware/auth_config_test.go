package middleware

import "testing"

func TestNewAuthMiddlewareValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{name: "missing domain", config: Config{Audience: "https://api.example.com"}},
		{name: "missing audience", config: Config{Domain: "tenant.example.com"}},
		{name: "domain with scheme", config: Config{Domain: "https://tenant.example.com", Audience: "api"}},
		{name: "domain with path", config: Config{Domain: "tenant.example.com/path", Audience: "api"}},
		{name: "domain with query", config: Config{Domain: "tenant.example.com?x=1", Audience: "api"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewAuthMiddleware(test.config); err == nil {
				t.Fatal("NewAuthMiddleware() succeeded, want configuration error")
			}
		})
	}
	auth, err := NewAuthMiddleware(Config{Domain: "tenant.example.com", Audience: "https://api.example.com"})
	if err != nil {
		t.Fatalf("NewAuthMiddleware(valid config) returned an error: %v", err)
	}
	if auth.Validator == nil || auth.validate != nil {
		t.Fatal("NewAuthMiddleware() must use the replaceable public Validator instead of caching its method value")
	}
	if _, err := NewAuthMiddlewareWithTokenValidator(nil); err == nil {
		t.Fatal("NewAuthMiddlewareWithTokenValidator(nil) succeeded")
	}
}
