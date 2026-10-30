package profile

import "testing"

func TestResolveFreeDefaults(t *testing.T) {
	got, err := Resolve("free", "", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != FreeURL || got.APIKey != "dummy" || got.ChunkPages != 20 || got.Workers != 1 {
		t.Fatalf("unexpected settings: %#v", got)
	}
}

func TestResolveAPIDefaults(t *testing.T) {
	got, err := Resolve("api", "", "sk-test", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != APIURL || got.ChunkPages != 300 || got.Workers != 4 {
		t.Fatalf("unexpected settings: %#v", got)
	}
}

func TestResolveAPIRequiresKey(t *testing.T) {
	if _, err := Resolve("api", "", "dummy", 0, 0); err == nil {
		t.Fatal("expected API key error")
	}
}
