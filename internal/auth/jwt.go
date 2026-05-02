// Package auth handles App Store Connect API authentication.
//
// Apple requires ES256 JWTs (ECDSA P-256 over SHA-256) signed against a .p8
// private key, with the signature in IEEE P1363 raw format (r||s, each 32
// bytes fixed-width) — NOT ASN.1 DER. This is the most common rolled-your-own
// gotcha; never use ecdsa.SignASN1 here.
package auth

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"
)

// Sentinel errors returned by Mint and helpers. Use errors.Is to test.
var (
	ErrKeyNotFound  = errors.New("auth: .p8 key file not found")
	ErrPermsTooWide = errors.New("auth: .p8 file permissions too wide")
	ErrInvalidPEM   = errors.New("auth: failed to decode PEM block from .p8 file")
	ErrInvalidKey   = errors.New("auth: parsed key is not an ECDSA P-256 private key")
	ErrEmptyKeyID   = errors.New("auth: keyID is empty")
)

// jwtTTL is Apple's hard ceiling for ASC API tokens. Mint fresh per request.
const jwtTTL = 20 * time.Minute

// p256SigSize is the IEEE P1363 raw signature size for P-256: r||s, 32 bytes each.
const p256SigSize = 64

// Mint signs an App Store Connect API JWT.
//
// keyID is the 10-char ASC API key id (e.g. "ABC1234DEF").
// issuerID is the issuer UUID from App Store Connect → Users and Access → Integrations.
// keyPath is the absolute path to AuthKey_<KEY_ID>.p8; it must be mode 0600 or stricter.
//
// The returned token is valid for 20 minutes (Apple's ceiling).
func Mint(keyID, issuerID, keyPath string) (string, error) {
	priv, err := loadKey(keyPath)
	if err != nil {
		return "", err
	}

	now := time.Now()
	header := map[string]string{
		"alg": "ES256",
		"kid": keyID,
		"typ": "JWT",
	}
	claims := map[string]any{
		"iss": issuerID,
		"iat": now.Unix(),
		"exp": now.Add(jwtTTL).Unix(),
		"aud": "appstoreconnect-v1",
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("auth: marshal header: %w", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("auth: marshal claims: %w", err)
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(claimsJSON)

	hash := sha256.Sum256([]byte(signingInput))

	r, s, err := ecdsa.Sign(rand.Reader, priv, hash[:])
	if err != nil {
		return "", fmt.Errorf("auth: ecdsa sign: %w", err)
	}

	// IEEE P1363 raw signature: r || s, fixed-width 32 bytes each (P-256).
	// Apple rejects ASN.1 DER signatures — never use ecdsa.SignASN1 here.
	sig := make([]byte, p256SigSize)
	r.FillBytes(sig[0:32])
	s.FillBytes(sig[32:64])

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// loadKey reads a .p8 file, verifies its mode, and parses out the ECDSA P-256 private key.
func loadKey(path string) (*ecdsa.PrivateKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrKeyNotFound, path)
		}
		return nil, fmt.Errorf("auth: stat %s: %w", path, err)
	}

	mode := info.Mode().Perm()
	if mode&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s has mode %#o (run `chmod 600 %s`)",
			ErrPermsTooWide, path, mode, path)
	}

	// #nosec G304 -- path is user-supplied config (env or flag); mode 0600 enforced above.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("auth: read %s: %w", path, err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, ErrInvalidPEM
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth: parse PKCS8: %w", err)
	}

	priv, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, ErrInvalidKey
	}
	return priv, nil
}
