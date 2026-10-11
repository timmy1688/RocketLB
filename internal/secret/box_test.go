// 本文件检查加密后再解密能得到原文，换一把密钥则解不开。
package secret

import (
	"bytes"
	"testing"
)

// TestRoundTrip 加密后再解密，并确认另一把密钥解不开。
func TestRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	box, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := box.Encrypt("ssh-secret")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "ssh-secret" || enc == "" {
		t.Fatalf("ciphertext = %q", enc)
	}
	got, err := box.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ssh-secret" {
		t.Fatalf("got %q", got)
	}
	empty, err := box.Encrypt("")
	if err != nil || empty != "" {
		t.Fatalf("empty encrypt: %q %v", empty, err)
	}
}
