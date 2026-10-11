package validate

import "testing"

func TestOverlappingName(t *testing.T) {
	if _, ok := OverlappingName("a.example.com", "b.example.com"); ok {
		t.Fatal("different names should share a port")
	}
	if name, ok := OverlappingName("www.example.com api.example.com", "api.example.com"); !ok || name != "api.example.com" {
		t.Fatalf("exact: %q %v", name, ok)
	}
	if _, ok := OverlappingName("*.example.com", "www.example.com"); !ok {
		t.Fatal("wildcard should cover a name")
	}
}
