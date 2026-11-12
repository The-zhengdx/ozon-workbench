package data

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// AESCipher encrypts API keys with AES-256-GCM. The stored bytes are nonce || ciphertext.
type AESCipher struct {
	key []byte
}

func NewCipher(raw string) (*AESCipher, error) {
	key, err := deriveKey(raw)
	if err != nil {
		return nil, err
	}
	return &AESCipher{key: key}, nil
}

func deriveKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("未设置 OZON_CREDENTIAL_KEY")
	}
	if len(raw) == 64 {
		if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

func (c *AESCipher) Encrypt(plain string) ([]byte, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plain), nil), nil
}

func (c *AESCipher) Decrypt(ciphertext []byte) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", fmt.Errorf("密文长度无效")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	plain, err := gcm.Open(nil, nonce, ciphertext[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("无法解密 API Key")
	}
	return string(plain), nil
}
