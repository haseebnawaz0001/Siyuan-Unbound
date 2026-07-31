// SiYuan - Refactor your thinking
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package util

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"testing"
)

// TestArgon2KDFConsistency verifies that the same password+salt+params always produce the same derived result.
func TestArgon2KDFConsistency(t *testing.T) {
	params := Argon2Params{Memory: 64 * 1024, Iterations: 3, Parallelism: 1, KeyLength: 32}
	salt := []byte("0123456789abcdef")
	k1 := DeriveKey("password", salt, params)
	k2 := DeriveKey("password", salt, params)
	if !bytes.Equal(k1, k2) {
		t.Fatalf("Argon2id derived keys differ for same inputs")
	}
	if len(k1) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(k1))
	}
}

// TestArgon2KDFDifferentPasswords verifies that different passwords derive different keys.
func TestArgon2KDFDifferentPasswords(t *testing.T) {
	params := Argon2Params{Memory: 64 * 1024, Iterations: 3, Parallelism: 1, KeyLength: 32}
	salt := []byte("0123456789abcdef")
	k1 := DeriveKey("password1", salt, params)
	k2 := DeriveKey("password2", salt, params)
	if bytes.Equal(k1, k2) {
		t.Fatalf("different passwords derived the same key")
	}
}

// TestAESGCMRoundTrip verifies that encrypt -> decrypt restores the original text.
func TestAESGCMRoundTrip(t *testing.T) {
	key, err := GenerateDEK()
	if err != nil {
		t.Fatalf("GenerateDEK failed: %v", err)
	}
	plaintext := []byte("hello 思源加密笔记本")
	ct, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	pt, err := Decrypt(key, ct)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if !bytes.Equal(plaintext, pt) {
		t.Fatalf("round-trip mismatch: got %q want %q", pt, plaintext)
	}
	if !bytes.Equal(ct[:len(encryptionMagic)], encryptionMagic[:]) {
		t.Fatalf("ciphertext is missing the encryption envelope magic")
	}
	if ct[len(encryptionMagic)] != EncryptionSpec {
		t.Fatalf("ciphertext spec mismatch: got %d want %d", ct[len(encryptionMagic)], EncryptionSpec)
	}
	nonce, nonceErr := EncryptionNonce(ct)
	if nonceErr != nil || len(nonce) != 12 {
		t.Fatalf("extract envelope nonce failed: nonce=%d err=%v", len(nonce), nonceErr)
	}
}

// TestAESGCMSamePlaintextDifferentCiphertext verifies that encrypting the same plaintext multiple times produces different results (random nonce).
func TestAESGCMSamePlaintextDifferentCiphertext(t *testing.T) {
	key, _ := GenerateDEK()
	plaintext := []byte("same content")
	ct1, _ := Encrypt(key, plaintext)
	ct2, _ := Encrypt(key, plaintext)
	if bytes.Equal(ct1, ct2) {
		t.Fatalf("same plaintext produced identical ciphertext (nonce not random?)")
	}
	// But both should still decrypt correctly
	if pt, err := Decrypt(key, ct1); err != nil || !bytes.Equal(pt, plaintext) {
		t.Fatalf("ct1 decrypt failed")
	}
	if pt, err := Decrypt(key, ct2); err != nil || !bytes.Equal(pt, plaintext) {
		t.Fatalf("ct2 decrypt failed")
	}
}

// TestAESGCMWrongKey verifies that decryption fails with the wrong key.
func TestAESGCMWrongKey(t *testing.T) {
	key1, _ := GenerateDEK()
	key2, _ := GenerateDEK()
	plaintext := []byte("secret")
	ct, _ := Encrypt(key1, plaintext)
	if _, err := Decrypt(key2, ct); err == nil {
		t.Fatalf("Decrypt with wrong key should fail")
	}
}

// TestAESGCMTamperedCiphertext verifies that decryption fails after the ciphertext has been tampered with (GCM integrity check).
func TestAESGCMTamperedCiphertext(t *testing.T) {
	key, _ := GenerateDEK()
	plaintext := []byte("integrity check")
	ct, _ := Encrypt(key, plaintext)
	// Flip the last byte (falls within the GCM tag region)
	ct[len(ct)-1] ^= 0xff
	if _, err := Decrypt(key, ct); err == nil {
		t.Fatalf("Decrypt of tampered ciphertext should fail")
	}
}

// TestAESGCMEnvelopeHeaderTampering verifies that the envelope header participates in authentication, and once tampered with, the data can no longer be read as valid ciphertext.
func TestAESGCMEnvelopeHeaderTampering(t *testing.T) {
	key, _ := GenerateDEK()
	ct, _ := Encrypt(key, []byte("header integrity"))
	ct[len(encryptionMagic)] ^= 0x01
	if _, err := Decrypt(key, ct); err == nil {
		t.Fatalf("Decrypt of ciphertext with tampered envelope header should fail")
	}
}

// TestAESGCMInvalidKeyLength verifies that a key that isn't 32 bytes is rejected.
func TestAESGCMInvalidKeyLength(t *testing.T) {
	shortKey := []byte("too-short")
	if _, err := Encrypt(shortKey, []byte("x")); err == nil {
		t.Fatalf("Encrypt with short key should fail")
	}
}

// TestAESGCMLegacyCiphertextCompatibility verifies that the legacy nonce||ciphertext||tag format can still be decrypted, ensuring existing data remains readable after upgrading.
func TestAESGCMLegacyCiphertextCompatibility(t *testing.T) {
	key, _ := GenerateDEK()
	plaintext := []byte("legacy encrypted notebook data")
	legacy, err := encryptLegacyForTest(key, plaintext, nil)
	if err != nil {
		t.Fatalf("create legacy ciphertext failed: %v", err)
	}
	got, err := Decrypt(key, legacy)
	if err != nil {
		t.Fatalf("decrypt legacy ciphertext failed: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("legacy round-trip mismatch")
	}
}

// TestAESGCMLegacyCiphertextWithAADCompatibility verifies that legacy file/asset ciphertext with AAD can still be read.
func TestAESGCMLegacyCiphertextWithAADCompatibility(t *testing.T) {
	key, _ := GenerateDEK()
	plaintext := []byte("legacy encrypted asset")
	aad := []byte("siyuan:v1:asset:box:assets/file.png")
	legacy, err := encryptLegacyForTest(key, plaintext, aad)
	if err != nil {
		t.Fatalf("create legacy ciphertext failed: %v", err)
	}
	got, err := DecryptWithAAD(key, legacy, aad)
	if err != nil {
		t.Fatalf("decrypt legacy ciphertext with AAD failed: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("legacy AAD round-trip mismatch")
	}
}

func encryptLegacyForTest(key, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

// TestGenerateSaltUnique verifies that the generated salt is sufficiently random (two calls produce different results).
func TestGenerateSaltUnique(t *testing.T) {
	s1, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt failed: %v", err)
	}
	if len(s1) != 16 {
		t.Fatalf("expected 16-byte salt, got %d", len(s1))
	}
	s2, _ := GenerateSalt()
	if bytes.Equal(s1, s2) {
		t.Fatalf("two salts should differ")
	}
}
