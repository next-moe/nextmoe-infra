package llm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

var errNoKEK = errors.New("KUN_LLM_KEK is not set")

func ParseKEK(b64 string) ([]byte, error) {
	if b64 == "" {
		return nil, nil
	}
	k, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("KUN_LLM_KEK: %w", err)
	}
	if len(k) != 32 {
		return nil, fmt.Errorf("KUN_LLM_KEK must decode to 32 bytes, got %d", len(k))
	}
	return k, nil
}

func SealKey(kek []byte, plain string) ([]byte, error) {
	if len(kek) == 0 {
		return nil, errNoKEK
	}
	gcm, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plain), nil), nil
}

func openKey(kek, sealed []byte) (string, error) {
	if len(kek) == 0 {
		return "", errNoKEK
	}
	gcm, err := newGCM(kek)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("sealed key too short")
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func newGCM(kek []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
