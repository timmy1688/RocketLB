package cluster

import "testing"

func TestVRIDFromVIP(t *testing.T) {
	if got := vridFromVIP("192.168.100.200"); got != 200 {
		t.Fatalf("got %d", got)
	}
	if got := vridFromVIP("10.0.0.0"); got != 51 {
		t.Fatalf("zero octet: %d", got)
	}
}

func TestRandomAuthPass(t *testing.T) {
	pass, err := randomAuthPass()
	if err != nil {
		t.Fatal(err)
	}
	if len(pass) != 8 {
		t.Fatalf("len %d", len(pass))
	}
	for _, r := range pass {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			t.Fatalf("bad char %q in %s", r, pass)
		}
	}
}
