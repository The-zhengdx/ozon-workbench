package data

import (
	"bytes"
	"testing"
)

func TestCipherRoundTripAndKeyForms(t *testing.T) {
	for _, raw := range []string{
		"dev-only-local-key-change-me-please",
		"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	} {
		c, err := NewCipher(raw)
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := c.Encrypt("api-key-值")
		if err != nil {
			t.Fatal(err)
		}
		again, err := c.Encrypt("api-key-值")
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(sealed, again) {
			t.Fatal("nonce was reused")
		}
		plain, err := c.Decrypt(sealed)
		if err != nil || plain != "api-key-值" {
			t.Fatalf("plain %q err %v", plain, err)
		}
	}
	if _, err := NewCipher("  "); err == nil {
		t.Fatal("empty key accepted")
	}
	a, _ := NewCipher("one")
	b, _ := NewCipher("two")
	sealed, _ := a.Encrypt("secret")
	if _, err := b.Decrypt(sealed); err == nil {
		t.Fatal("other key decrypted the secret")
	}
}
