package staticgen

import "testing"

func TestAnyLiteralPreservesFloat32Precision(t *testing.T) {
	literal, err := anyLiteral(float32(0.1))
	if err != nil {
		t.Fatalf("anyLiteral(float32) returned an error: %v", err)
	}
	if got, want := literal, "float32(0.1)"; got != want {
		t.Fatalf("float32 literal = %q, want %q", got, want)
	}
}

func TestAnyLiteralTypesLargeUnsignedInteger(t *testing.T) {
	literal, err := anyLiteral(uint64(^uint64(0)))
	if err != nil {
		t.Fatalf("anyLiteral(uint64) returned an error: %v", err)
	}
	if got, want := literal, "uint64(18446744073709551615)"; got != want {
		t.Fatalf("uint64 literal = %q, want %q", got, want)
	}
}
