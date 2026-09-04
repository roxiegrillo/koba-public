package koba_test

import (
	"testing"

	"github.com/roxiegrillo/koba/v2/kobaapp"
)

func TestStaticFixtureArtifactsAreCurrent(t *testing.T) {
	if err := kobaapp.Check(staticFixtureDefinition()); err != nil {
		t.Fatalf("kobaapp.Check() returned an error: %v", err)
	}
}
