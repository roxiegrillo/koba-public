package koba

import "testing"

func TestAuthenticatedAppConstructorsFailClosed(t *testing.T) {
	if _, err := NewAuthenticatedApp("", "api"); err == nil {
		t.Fatal("NewAuthenticatedApp() succeeded with an empty domain")
	}
	if _, err := NewAuthenticatedApp("tenant.example.com", ""); err == nil {
		t.Fatal("NewAuthenticatedApp() succeeded with an empty audience")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MakeAuthenticatedApp() did not panic on invalid configuration")
		}
	}()
	_ = MakeAuthenticatedApp("", "api")
}

func TestNewAuthenticatedAppAcceptsValidConfiguration(t *testing.T) {
	app, err := NewAuthenticatedApp("tenant.example.com", "https://api.example.com")
	if err != nil {
		t.Fatalf("NewAuthenticatedApp() returned an error: %v", err)
	}
	if app == nil {
		t.Fatal("NewAuthenticatedApp() returned a nil app")
	}
	app.AddApi(&scopedRegistrationAPI{})
}
