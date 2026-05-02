package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTestKey generates an ephemeral P-256 key, marshals it as PKCS8 PEM, and
// writes it to a tempfile at the requested mode. Returns (path, public key).
// The key never leaves the test process — never commit a real .p8.
func writeTestKey(t *testing.T, mode os.FileMode) (string, *ecdsa.PublicKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal PKCS8: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	path := filepath.Join(t.TempDir(), "AuthKey_TEST123ABC.p8")
	if err := os.WriteFile(path, pemBytes, mode); err != nil {
		t.Fatalf("write test key: %v", err)
	}
	// os.WriteFile honors umask, so chmod explicitly to lock the mode in.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod test key: %v", err)
	}
	return path, &priv.PublicKey
}

func TestMint_ProducesValidJWT(t *testing.T) {
	path, pub := writeTestKey(t, 0o600)

	const keyID = "TEST123ABC"
	const issuerID = "11111111-2222-3333-4444-555555555555"

	token, err := Mint(keyID, issuerID, path)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT parts, got %d", len(parts))
	}

	// --- header ---
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header map[string]string
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if got, want := header["alg"], "ES256"; got != want {
		t.Errorf("header.alg = %q, want %q", got, want)
	}
	if got, want := header["kid"], keyID; got != want {
		t.Errorf("header.kid = %q, want %q", got, want)
	}
	if got, want := header["typ"], "JWT"; got != want {
		t.Errorf("header.typ = %q, want %q", got, want)
	}

	// --- claims ---
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
		Aud string `json:"aud"`
	}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Iss != issuerID {
		t.Errorf("claims.iss = %q, want %q", claims.Iss, issuerID)
	}
	if claims.Aud != "appstoreconnect-v1" {
		t.Errorf("claims.aud = %q, want appstoreconnect-v1", claims.Aud)
	}
	now := time.Now().Unix()
	if claims.Iat > now+5 || claims.Iat < now-5 {
		t.Errorf("claims.iat skew too large: %d vs now %d", claims.Iat, now)
	}
	if want := claims.Iat + int64(jwtTTL.Seconds()); claims.Exp != want {
		t.Errorf("claims.exp = %d, want %d (iat + 20m)", claims.Exp, want)
	}

	// --- signature: must be IEEE P1363 raw 64 bytes, NOT DER ---
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	if len(sig) != p256SigSize {
		t.Fatalf("sig length = %d, want %d (IEEE P1363 r||s for P-256)", len(sig), p256SigSize)
	}

	// Verify the signature actually validates against the public key.
	r := new(big.Int).SetBytes(sig[0:32])
	s := new(big.Int).SetBytes(sig[32:64])
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, hash[:], r, s) {
		t.Error("ecdsa.Verify rejected the signature; sig may be malformed or DER-encoded")
	}
}

func TestMint_RefusesWideMode(t *testing.T) {
	path, _ := writeTestKey(t, 0o644)

	_, err := Mint("TEST", "ISSUER", path)
	if !errors.Is(err, ErrPermsTooWide) {
		t.Fatalf("err = %v, want ErrPermsTooWide", err)
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("error message missing chmod hint: %v", err)
	}
}

func TestMint_MissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "AuthKey_MISSING.p8")

	_, err := Mint("MISSING", "ISSUER", missing)
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("err = %v, want ErrKeyNotFound", err)
	}
}

func TestMint_RejectsRSAKey(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatalf("marshal PKCS8: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	path := filepath.Join(t.TempDir(), "AuthKey_RSA.p8")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write RSA key: %v", err)
	}

	_, err = Mint("RSA", "ISSUER", path)
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("err = %v, want ErrInvalidKey", err)
	}
}

func TestMint_RejectsBadPEM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AuthKey_BAD.p8")
	if err := os.WriteFile(path, []byte("not a PEM block"), 0o600); err != nil {
		t.Fatalf("write bad: %v", err)
	}

	_, err := Mint("BAD", "ISSUER", path)
	if !errors.Is(err, ErrInvalidPEM) {
		t.Fatalf("err = %v, want ErrInvalidPEM", err)
	}
}

func TestKeyPath_EnvOverride(t *testing.T) {
	t.Setenv(envKeyPathOverride, "/tmp/custom/path.p8")

	got, err := KeyPath("ANYTHING")
	if err != nil {
		t.Fatalf("KeyPath: %v", err)
	}
	if got != "/tmp/custom/path.p8" {
		t.Errorf("KeyPath = %q, want /tmp/custom/path.p8", got)
	}
}

func TestKeyPath_DefaultLocation(t *testing.T) {
	t.Setenv(envKeyPathOverride, "")

	got, err := KeyPath("ABC1234DEF")
	if err != nil {
		t.Fatalf("KeyPath: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	want := filepath.Join(home, ".appstoreconnect", "AuthKey_ABC1234DEF.p8")
	if got != want {
		t.Errorf("KeyPath = %q, want %q", got, want)
	}
}

func TestKeyPath_EmptyKeyIDWithoutOverride(t *testing.T) {
	t.Setenv(envKeyPathOverride, "")

	_, err := KeyPath("")
	if !errors.Is(err, ErrEmptyKeyID) {
		t.Fatalf("err = %v, want ErrEmptyKeyID", err)
	}
}
