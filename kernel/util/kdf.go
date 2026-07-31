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
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
)

var encryptionMagic = [4]byte{'S', 'E', 'N', 'C'}

const (
	// EncryptionSpec represents the AES-GCM ciphertext envelope spec version. Legacy ciphertext has no envelope header; reading stays compatible with it.
	EncryptionSpec byte = 1

	encryptionAlgorithmAES256GCM byte = 1
	encryptionEnvelopeHeaderSize      = len(encryptionMagic) + 3 // magic + spec + algorithm + nonce length
)

// Argon2Params holds the parameters for the Argon2id key derivation function. The parameters themselves are not
// secret and are persisted with the configuration, so keys can be derived consistently across platforms.
// The defaults follow the OWASP 2023 recommendation (64 MB memory / 3 iterations / 4 threads / 32-byte output).
type Argon2Params struct {
	Memory      uint32 `json:"memory"`      // Memory used per derivation, in KB
	Iterations  uint32 `json:"iterations"`  // Number of passes over memory
	Parallelism uint8  `json:"parallelism"` // Number of parallel threads
	KeyLength   uint32 `json:"keyLength"`   // Output key length, in bytes
}

// DefaultArgon2Params returns the OWASP 2023-recommended Argon2id parameters.
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 4,
		KeyLength:   32,
	}
}

// ValidateArgon2Params validates whether the Argon2id parameters are within a reasonable range.
// When KeyLength is 0 it is treated as a legacy config and the defaults are returned; a non-zero but invalid
// parameter returns an error.
// This prevents a malicious backup from setting an extremely large memory value to cause OOM, or from setting
// overly weak parameters that reduce security.
func ValidateArgon2Params(p Argon2Params) (Argon2Params, error) {
	if p.KeyLength == 0 {
		return DefaultArgon2Params(), nil
	}
	if p.KeyLength != 32 {
		return p, errors.New("Argon2id KeyLength must be 32")
	}
	if p.Memory < 64*1024 {
		return p, errors.New("Argon2id Memory too low (minimum 64 MB)")
	}
	if p.Memory > 256*1024 {
		return p, errors.New("Argon2id Memory too high (maximum 256 MB)")
	}
	if p.Iterations < 3 {
		return p, errors.New("Argon2id Iterations too low (minimum 3)")
	}
	if p.Iterations > 10 {
		return p, errors.New("Argon2id Iterations too high (maximum 10)")
	}
	if p.Parallelism == 0 || p.Parallelism > 16 {
		return p, errors.New("Argon2id Parallelism must be between 1 and 16")
	}
	return p, nil
}

// DeriveKey derives a key from a password using Argon2id. The same password+salt+params always produce the same result.
func DeriveKey(password string, salt []byte, p Argon2Params) []byte {
	return argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)
}

// Encrypt encrypts with AES-256-GCM. Each call generates a random nonce, so encrypting the same plaintext
// multiple times produces different results.
// Return format: magic(4B) || spec(1B) || algorithm(1B) || nonceLength(1B) || nonce || ciphertext || GCM tag(16B).
func Encrypt(key, plaintext []byte) ([]byte, error) {
	return encryptGCM(key, plaintext, nil, "Encrypt")
}

// Decrypt is the decryption counterpart to Encrypt. Compatible with the legacy nonce||ciphertext||tag format.
// Returns an error if the key is wrong or the ciphertext has been tampered with (GCM has built-in integrity checking).
func Decrypt(key, ciphertext []byte) ([]byte, error) {
	return decryptGCM(key, ciphertext, nil, "Decrypt")
}

// EncryptionNonce extracts the nonce from AES-GCM ciphertext, compatible with the legacy format without an envelope header.
func EncryptionNonce(ciphertext []byte) ([]byte, error) {
	if hasEncryptionMagic(ciphertext) {
		if len(ciphertext) < encryptionEnvelopeHeaderSize {
			return nil, errors.New("encrypted envelope too short")
		}
		if ciphertext[len(encryptionMagic)] != EncryptionSpec {
			return nil, errors.New("unsupported encrypted envelope spec")
		}
		if ciphertext[len(encryptionMagic)+1] != encryptionAlgorithmAES256GCM {
			return nil, errors.New("unsupported encrypted envelope algorithm")
		}
		nonceLength := int(ciphertext[len(encryptionMagic)+2])
		if nonceLength == 0 || len(ciphertext) < encryptionEnvelopeHeaderSize+nonceLength {
			return nil, errors.New("invalid encrypted envelope nonce length")
		}
		return append([]byte(nil), ciphertext[encryptionEnvelopeHeaderSize:encryptionEnvelopeHeaderSize+nonceLength]...), nil
	}
	if len(ciphertext) < 12 {
		return nil, errors.New("ciphertext too short to extract nonce")
	}
	return append([]byte(nil), ciphertext[:12]...), nil
}

// DeriveSubKey derives a purpose-isolated subkey from the master DEK using HKDF-SHA256.
// The same (dek, purpose) always produces the same result; different purposes derive mutually independent
// subkeys, achieving purpose separation -- .sy/assets/AV each use their own subkey, none interchangeable, which
// limits the blast radius of a single key leak.
func DeriveSubKey(dek []byte, purpose string) []byte {
	// Use the purpose bytes as HKDF info; salt is nil (the DEK itself is already a high-entropy random key, no extra salt needed)
	r := hkdf.New(sha256.New, dek, nil, []byte(purpose))
	out := make([]byte, 32) // AES-256
	if _, err := io.ReadFull(r, out); err != nil {
		// hkdf.Read should not fail (unless dek is empty); defensive panic to avoid silently returning a weak key
		panic("hkdf derive failed: " + err.Error())
	}
	return out
}

// EncryptWithAAD encrypts with AES-256-GCM and binds AAD (additional authenticated data).
// AAD is not encrypted but participates in GCM authentication -- decryption must supply the same AAD, or
// authentication fails.
// Putting metadata like purpose/boxID/path into AAD prevents ciphertext within the same box from being swapped
// between purposes or paths (bound to context).
// The return format matches Encrypt, but AAD participates in verification.
func EncryptWithAAD(key, plaintext, aad []byte) ([]byte, error) {
	return encryptGCM(key, plaintext, aad, "EncryptWithAAD")
}

// DecryptWithAAD is the decryption counterpart to EncryptWithAAD, compatible with the legacy
// nonce||ciphertext||tag format.
// Returns an error if AAD doesn't match or the ciphertext has been tampered with.
func DecryptWithAAD(key, ciphertext, aad []byte) ([]byte, error) {
	return decryptGCM(key, ciphertext, aad, "DecryptWithAAD")
}

func encryptGCM(key, plaintext, aad []byte, operation string) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New(operation + " requires a 32-byte (AES-256) key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	nonce := make([]byte, nonceSize)
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	envelope := make([]byte, encryptionEnvelopeHeaderSize, encryptionEnvelopeHeaderSize+nonceSize+len(plaintext)+gcm.Overhead())
	copy(envelope, encryptionMagic[:])
	envelope[len(encryptionMagic)] = EncryptionSpec
	envelope[len(encryptionMagic)+1] = encryptionAlgorithmAES256GCM
	envelope[len(encryptionMagic)+2] = byte(nonceSize)
	envelope = append(envelope, nonce...)
	return gcm.Seal(envelope, nonce, plaintext, envelopeAAD(envelope[:encryptionEnvelopeHeaderSize], aad)), nil
}

func decryptGCM(key, ciphertext, aad []byte, operation string) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New(operation + " requires a 32-byte (AES-256) key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if hasEncryptionMagic(ciphertext) {
		if len(ciphertext) < encryptionEnvelopeHeaderSize {
			return nil, errors.New("encrypted envelope too short")
		}
		if ciphertext[len(encryptionMagic)] != EncryptionSpec {
			return nil, errors.New("unsupported encrypted envelope spec")
		}
		if ciphertext[len(encryptionMagic)+1] != encryptionAlgorithmAES256GCM {
			return nil, errors.New("unsupported encrypted envelope algorithm")
		}
		if int(ciphertext[len(encryptionMagic)+2]) != nonceSize {
			return nil, errors.New("invalid encrypted envelope nonce length")
		}
		if len(ciphertext) < encryptionEnvelopeHeaderSize+nonceSize+gcm.Overhead() {
			return nil, errors.New("encrypted envelope too short")
		}
		nonce := ciphertext[encryptionEnvelopeHeaderSize : encryptionEnvelopeHeaderSize+nonceSize]
		ct := ciphertext[encryptionEnvelopeHeaderSize+nonceSize:]
		return gcm.Open(nil, nonce, ct, envelopeAAD(ciphertext[:encryptionEnvelopeHeaderSize], aad))
	}
	if len(ciphertext) < nonceSize+gcm.Overhead() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ct, aad)
}

func hasEncryptionMagic(ciphertext []byte) bool {
	return len(ciphertext) >= len(encryptionMagic) && bytes.Equal(ciphertext[:len(encryptionMagic)], encryptionMagic[:])
}

// IsCiphertext determines whether the given bytes start with the encryption envelope magic (i.e. whether it's
// ciphertext).
// Used by paths like history indexing that can't obtain the boxID/DEK, as a defensive check: when ciphertext is
// encountered, skip parsing instead of erroring out as invalid JSON, avoiding noisy errors when an encrypted
// notebook's AV or other objects end up at a global location due to path migration (sync, import, legacy layout
// changes).
func IsCiphertext(data []byte) bool {
	return hasEncryptionMagic(data)
}

// envelopeAAD folds the public envelope header and the caller's AAD together into GCM authentication, preventing the spec or algorithm identifier from being tampered with.
func envelopeAAD(header, aad []byte) []byte {
	ret := make([]byte, 0, len(header)+len(aad))
	ret = append(ret, header...)
	return append(ret, aad...)
}

// GenerateSalt generates a random salt (16 bytes).
func GenerateSalt() ([]byte, error) {
	return randomBytes(16)
}

// GenerateDEK generates a random data encryption key (32 bytes, AES-256).
func GenerateDEK() ([]byte, error) {
	return randomBytes(32)
}

// randomBytes reads the given number of random bytes from crypto/rand.
func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}
