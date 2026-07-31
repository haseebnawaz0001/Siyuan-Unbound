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

package filesys

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/88250/lute/ast"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// DEKProvider is injected by the model layer at init time, used to look up the DEK for a boxID.
// Returning (nil, nil) means the box is not encrypted -- the encrypt/decrypt functions return data as-is,
// transparent to ordinary notebooks.
// Returning (nil, error) means it's encrypted but not unlocked -- the encrypt/decrypt functions refuse to
// read/write, to avoid silently writing plaintext to disk while unlocked.
// filesys cannot import model directly (that would form a model -> filesys -> model circular dependency), so
// callback injection is used instead.
var DEKProvider func(boxID string) ([]byte, error)

// DEKLockAcquire / DEKLockRelease are injected by the model layer, holding the box's read lock before and
// after obtaining the DEK, to prevent LockBox from clearing the cache during encryption/decryption. When the
// injection is nil for a non-encrypted box, behavior is unaffected.
var DEKLockAcquire func(boxID string)
var DEKLockRelease func(boxID string)

// SyObjectBase extracts the stable file basename from a box-relative path and validates it.
// It accepts a basename of the form <rootID>.sy: the extension must be .sy, and the stem must be a valid node ID.
// An invalid extension or a stem that doesn't match the node ID pattern returns an error, to avoid treating an
// arbitrary path as the AAD binding and producing data that cannot be decrypted.
// Used together by filesys, model's history view/rollback, import, and all other .sy encrypt/decrypt paths, to
// keep the AAD consistent.
func SyObjectBase(relativePath string) (string, error) {
	p := filepath.ToSlash(relativePath)
	p = strings.TrimPrefix(p, "/")
	base := p
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		base = p[idx+1:]
	}
	if !strings.HasSuffix(base, ".sy") {
		return "", fmt.Errorf("invalid .sy base name [%s]: must end with .sy", base)
	}
	stem := strings.TrimSuffix(base, ".sy")
	if !ast.IsNodeIDPattern(stem) {
		return "", fmt.Errorf("invalid .sy base name [%s]: stem is not a node ID", base)
	}
	return base, nil
}

// SyAAD constructs the AAD for .sy ciphertext: siyuan:v1:file:<boxID>:<stable file basename>.
// The parent directory does not go into the AAD -- a move within the same box that keeps the filename
// unchanged is allowed to Rename the ciphertext as-is, while content/box/type/object ID are still authenticated.
func SyAAD(boxID, relativePath string) (string, error) {
	base, err := SyObjectBase(relativePath)
	if err != nil {
		return "", err
	}
	return "siyuan:v1:file:" + boxID + ":" + base, nil
}

// encryptedBox determines whether boxID is an already-unlocked encrypted box, for filesys's internal branching
// (such as disabling silent repair).
// Detected via DEKProvider: a non-nil dek means it's encrypted and unlocked.
func encryptedBox(boxID string) bool {
	if DEKProvider == nil {
		return false
	}
	if DEKLockAcquire != nil {
		DEKLockAcquire(boxID)
		defer DEKLockRelease(boxID)
	}
	dek, err := DEKProvider(boxID)
	return err == nil && dek != nil
}

// encryptData encrypts data with fileKey (a subkey derived from the DEK) if boxID is an already-unlocked
// encrypted box, with the AAD bound to boxID + the stable file basename (excluding the parent directory);
// a non-encrypted notebook is returned as-is; when encrypted but not unlocked, returns an error and refuses to
// write to disk (to prevent plaintext leaks).
func encryptData(boxID, relativePath string, data []byte) ([]byte, error) {
	if DEKProvider == nil {
		return data, nil
	}
	if DEKLockAcquire != nil {
		DEKLockAcquire(boxID)
		defer DEKLockRelease(boxID)
	}
	dek, err := DEKProvider(boxID)
	if err != nil {
		return nil, err
	}
	if dek == nil {
		return data, nil // not an encrypted box
	}
	fileKey := util.DeriveSubKey(dek, "siyuan/file")
	aad, err := SyAAD(boxID, relativePath)
	if err != nil {
		return nil, err
	}
	return util.EncryptWithAAD(fileKey, data, []byte(aad))
}

// decryptData is the corresponding decryption. A non-encrypted notebook is returned as-is; when encrypted but not unlocked, returns an error and refuses to read from disk.
func decryptData(boxID, relativePath string, data []byte) ([]byte, error) {
	if DEKProvider == nil {
		return data, nil
	}
	if DEKLockAcquire != nil {
		DEKLockAcquire(boxID)
		defer DEKLockRelease(boxID)
	}
	dek, err := DEKProvider(boxID)
	if err != nil {
		return nil, err
	}
	if dek == nil {
		return data, nil // not an encrypted box
	}
	fileKey := util.DeriveSubKey(dek, "siyuan/file")
	aad, err := SyAAD(boxID, relativePath)
	if err != nil {
		return nil, err
	}
	return util.DecryptWithAAD(fileKey, data, []byte(aad))
}

// docIALBoxID infers the boxID from a .sy absolute path, for DocIAL to determine whether full decryption is needed.
// The path is of the form <DataDir>/<boxID>/...; returns an empty string if it's not under DataDir or boxID doesn't match a valid ID pattern.
func docIALBoxID(absPath string) string {
	absPath = filepath.ToSlash(absPath)
	dataDir := filepath.ToSlash(util.DataDir)
	rel, err := filepath.Rel(dataDir, absPath)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "..") || rel == "." || rel == "" {
		return ""
	}
	parts := strings.SplitN(rel, "/", 2)
	boxID := parts[0]
	if !ast.IsNodeIDPattern(boxID) {
		return ""
	}
	return boxID
}
