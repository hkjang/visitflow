package platform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

type Keyring struct{ key []byte }

var (
	visitFlowAAD = []byte("visitflow-data-v1")
	legacyAAD    = []byte("seaton-setting-v1")
)

func NewKeyringFromSecret(secret string) (*Keyring, error) {
	secret = strings.TrimSpace(secret)
	var key []byte
	if decoded, err := hex.DecodeString(secret); err == nil && len(decoded) == 32 {
		key = decoded
	} else {
		encodings := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}
		for _, encoding := range encodings {
			decoded, decodeErr := encoding.DecodeString(secret)
			if decodeErr == nil && len(decoded) == 32 {
				key = decoded
				break
			}
		}
	}
	if len(key) != 32 {
		return nil, errors.New("ENCRYPTION_KEY must encode exactly 32 bytes (base64 or 64-character hex)")
	}
	return &Keyring{key: append([]byte(nil), key...)}, nil
}

func (k *Keyring) Encrypt(plain string) (string, error) {
	block, err := aes.NewCipher(k.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), visitFlowAAD)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func (k *Keyring) Decrypt(encoded string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(b) < gcm.NonceSize() {
		return "", errors.New("encrypted value is truncated")
	}
	plain, err := gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], visitFlowAAD)
	if err != nil {
		// v2.0.0 used the former project AAD. The fallback keeps upgrades
		// readable while all newly encrypted values use the VisitFlow AAD.
		plain, err = gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], legacyAAD)
	}
	return string(plain), err
}

func (k *Keyring) Digest(value string) []byte {
	h := hmac.New(sha256.New, k.key)
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}

func RandomToken(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
