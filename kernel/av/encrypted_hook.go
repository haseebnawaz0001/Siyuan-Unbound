// SiYuan - Refactor your thinking
// Copyright (c) 2020-present, b3log.org
//
// This file provides notebook-level storage and DEK encrypt/decrypt support for AV definitions
// in encrypted notebooks.
// Same pattern as filesys/crypto_hook.go: the av package does not import model directly (to avoid
// circular dependencies); the model layer injects the callback functions at init.

package av

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/88250/lute/ast"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/util"
	"github.com/vmihailenco/msgpack/v5"
)

// AVDEKProvider is injected by the model layer; it returns the DEK of an unlocked encrypted
// notebook.
// Returning (nil, nil) means the box is not encrypted or is not unlocked -- in that case the AV
// definition is stored as plaintext, transparent to regular notebooks.
var AVDEKProvider func(boxID string) ([]byte, error)

// AVLockAcquire / AVLockRelease are injected by the model layer to hold the box read lock before
// and after obtaining the DEK, preventing LockBox from clearing the cache during AV encryption or
// decryption. Behavior is unaffected when the injection is nil for a non-encrypted box.
var AVLockAcquire func(boxID string)
var AVLockRelease func(boxID string)

// AVEncryptedBoxIDs is injected by the model layer; it returns the list of all currently opened
// encrypted box IDs.
// Used when iterating over AV path fallbacks.
var AVEncryptedBoxIDs func() []string

// AVIsEncryptedBox is injected by the model layer; it reports whether boxID is an encrypted notebook.
var AVIsEncryptedBox func(boxID string) bool

// AVGetBlockBoxID is injected by the model layer; it returns the boxID that blockID belongs to
// (looked up via the blocktree).
// Used to verify that the source block and the AV definition are within the same encryption
// boundary when writing mirrors.
var AVGetBlockBoxID func(blockID string) string

// pendingAVBox records which encrypted box a newly created AV belongs to.
// The handler layer calls SetAVBoxID(avID, boxID) before creating the AV; when SaveAttributeView
// runs, findAttributeViewPath first checks the pending map and, if found, writes to the
// corresponding encrypted notebook path.
var pendingAVBox = map[string]string{}
var pendingAVBoxLock = sync.RWMutex{}

// SetAVBoxID presets the owning box of an AV definition. Called when an encrypted notebook creates
// an AV; clears the mapping when boxID is empty.
// Regular notebooks don't need to call this (AV defaults to the global path).
func SetAVBoxID(avID, boxID string) {
	pendingAVBoxLock.Lock()
	defer pendingAVBoxLock.Unlock()
	if boxID != "" {
		pendingAVBox[avID] = boxID
	} else {
		delete(pendingAVBox, avID)
	}
}

// GetAVBoxID looks up the owning box of an AV definition (used by the handler layer for decisions).
func GetAVBoxID(avID string) string {
	pendingAVBoxLock.RLock()
	defer pendingAVBoxLock.RUnlock()
	return pendingAVBox[avID]
}

// attributeViewDataPathByBox returns the AV definition path for the given box.
// Encrypted box: <DataDir>/<boxID>/storage/av/<avID>.json
// Regular box (boxID empty): <DataDir>/storage/av/<avID>.json
func attributeViewDataPathByBox(avID, boxID string) string {
	if !ast.IsNodeIDPattern(avID) || (boxID != "" && !ast.IsNodeIDPattern(boxID)) {
		return ""
	}

	if boxID != "" {
		return filepath.Join(util.DataDir, boxID, "storage", "av", avID+".json")
	}
	return filepath.Join(util.DataDir, "storage", "av", avID+".json")
}

// FindAttributeViewPath looks up the actual path of the AV definition file using fallback logic.
//  1. First check pendingAVBox (the boxID preset at first creation), without checking whether the
//     file exists (first-creation scenario)
//  2. Check the global storage/av/ (regular box)
//  3. Iterate over already-opened encrypted notebooks
//
// Returns the found path and the corresponding boxID (regular box returns an empty boxID). Returns
// an empty string if not found.
func FindAttributeViewPath(avID string) (path string, boxID string) {
	if !ast.IsNodeIDPattern(avID) {
		return
	}

	// Check pendingAVBox first (first-creation scenario); doesn't require the file to exist
	if pendingBoxID := GetAVBoxID(avID); pendingBoxID != "" {
		encPath := attributeViewDataPathByBox(avID, pendingBoxID)
		// If pending exists, return that box's path directly (the file doesn't exist yet on first creation)
		return encPath, pendingBoxID
	}
	// Check the global path
	globalPath := attributeViewDataPathByBox(avID, "")
	if filelock.IsExist(globalPath) {
		return globalPath, ""
	}
	// Iterate over already-opened encrypted notebooks
	if AVEncryptedBoxIDs != nil {
		for _, encBoxID := range AVEncryptedBoxIDs() {
			encPath := attributeViewDataPathByBox(avID, encBoxID)
			if filelock.IsExist(encPath) {
				return encPath, encBoxID
			}
		}
	}
	return "", ""
}

// FindAttributeViewPathInBox looks up the AV definition only within the specified box, to avoid
// an encrypted context falling back to the global path.
func FindAttributeViewPathInBox(avID, boxID string) (path string, retBoxID string) {
	if !ast.IsNodeIDPattern(avID) || (boxID != "" && !ast.IsNodeIDPattern(boxID)) {
		return
	}

	if pendingBoxID := GetAVBoxID(avID); pendingBoxID != "" {
		if pendingBoxID == boxID {
			// pending matches the target box, return directly (the file doesn't exist yet on first creation)
			return attributeViewDataPathByBox(avID, pendingBoxID), pendingBoxID
		}
		// The pending mapping belongs to another box; don't let it shadow this box's file lookup, keep checking disk
	}
	avPath := attributeViewDataPathByBox(avID, boxID)
	if filelock.IsExist(avPath) {
		return avPath, boxID
	}
	return "", boxID
}

// readAttributeViewData reads AV definition data using fallback logic (with automatic decryption).
func readAttributeViewData(avID string) ([]byte, error) {
	path, boxID := FindAttributeViewPath(avID)
	if path == "" {
		return nil, nil // File doesn't exist; let the caller handle it
	}
	data, err := filelock.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Data from encrypted notebooks needs to be decrypted
	if boxID != "" {
		data, err = decryptAVData(boxID, avID, data)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

// ReadAttributeViewData is the exported version of readAttributeViewData, for external packages
// such as model.export to read the plaintext AV definition (including automatic decryption of
// notebook-level AVs in encrypted notebooks).
func ReadAttributeViewData(avID string) ([]byte, error) {
	return readAttributeViewData(avID)
}

// ReadAttributeViewDataInBox reads the plaintext AV definition only within the specified box.
func ReadAttributeViewDataInBox(avID, boxID string) ([]byte, error) {
	path, retBoxID := FindAttributeViewPathInBox(avID, boxID)
	if path == "" {
		return nil, nil
	}
	data, err := filelock.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if retBoxID != "" {
		data, err = decryptAVData(retBoxID, avID, data)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}

// writeAttributeViewData writes AV definition data (with automatic encryption).
// When boxID is empty, writes to the global path (regular box); when non-empty, writes to the
// encrypted notebook path and encrypts the data.
func writeAttributeViewData(avID, boxID string, data []byte) error {
	if !ast.IsNodeIDPattern(avID) {
		return ErrInvalidAttributeViewID
	}
	if boxID != "" && !ast.IsNodeIDPattern(boxID) {
		return ErrInvalidBoxID
	}

	path := attributeViewDataPathByBox(avID, boxID)
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Data for encrypted notebooks needs to be encrypted
	if boxID != "" {
		var err error
		data, err = encryptAVData(boxID, avID, data)
		if err != nil {
			return err
		}
	}
	return filelock.WriteFile(path, data)
}

// mirrorBlocksPath returns the path of the mirror index file.
// Encrypted box: <DataDir>/<boxID>/storage/av/blocks.msgpack
// Regular box: <DataDir>/storage/av/blocks.msgpack
func mirrorBlocksPath(boxID string) string {
	if boxID != "" {
		return filepath.Join(util.DataDir, boxID, "storage", "av", "blocks.msgpack")
	}
	return filepath.Join(util.DataDir, "storage", "av", "blocks.msgpack")
}

// mirrorBlocksPathByAvID returns the mirror index path via the owning box of the AV definition.
// First checks findAttributeViewPath (including the pendingAVBox fallback); if found, returns the
// mirror path for that box.
// Returns the global path if not found.
func mirrorBlocksPathByAvID(avID string) string {
	_, boxID := FindAttributeViewPath(avID)
	return mirrorBlocksPath(boxID)
}

// readMirrorBlocks reads the mirror index by path (reads global when boxID is empty, reads the
// encrypted box when non-empty).
// The mirror index of an encrypted notebook is ciphertext encrypted with the DEK and needs to be
// decrypted after reading.
func readMirrorBlocks(boxID string) (ret map[string][]string) {
	ret = map[string][]string{}
	p := mirrorBlocksPath(boxID)
	if !filelock.IsExist(p) {
		return
	}
	data, err := filelock.ReadFile(p)
	if err != nil {
		logging.LogErrorf("read attribute view blocks failed: %s", err)
		return
	}
	if boxID != "" {
		// The mirror index of an encrypted notebook is ciphertext; decrypt before unmarshaling
		dec, decErr := decryptAVData(boxID, "mirror", data)
		if decErr != nil {
			logging.LogErrorf("decrypt attribute view blocks failed: %s", decErr)
			return
		}
		data = dec
	}
	if err = msgpack.Unmarshal(data, &ret); err != nil {
		logging.LogErrorf("unmarshal attribute view blocks failed: %s", err)
		return
	}
	return
}

// writeMirrorBlocks writes the mirror index by path.
// The mirror index of an encrypted notebook is encrypted with the DEK before writing.
func writeMirrorBlocks(boxID string, data map[string][]string) error {
	p := mirrorBlocksPath(boxID)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	raw, err := msgpack.Marshal(data)
	if err != nil {
		return err
	}
	if boxID != "" {
		// Encrypt the mirror index of an encrypted notebook before writing
		enc, encErr := encryptAVData(boxID, "mirror", raw)
		if encErr != nil {
			return encErr
		}
		raw = enc
	}
	return filelock.WriteFile(p, raw)
}

// Paths of the form <DataDir>/<boxID>/storage/av/<avID>.json -> returns boxID.
// The global path <DataDir>/storage/av/<avID>.json -> returns an empty string.
func avBoxIDFromPath(absPath string) string {
	rel, err := filepath.Rel(util.DataDir, absPath)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	parts := strings.SplitN(rel, "/", 2)
	if len(parts) < 2 {
		return ""
	}
	boxID := parts[0]
	// The first segment of a global path is "storage"; the first segment of an encrypted notebook
	// path is a boxID (node ID format)
	if boxID == "storage" {
		return ""
	}
	return boxID
}

func encryptAVData(boxID, avID string, data []byte) ([]byte, error) {
	if AVDEKProvider == nil {
		return data, nil
	}
	if AVLockAcquire != nil {
		AVLockAcquire(boxID)
		defer AVLockRelease(boxID)
	}
	dek, err := AVDEKProvider(boxID)
	if err != nil {
		return nil, err // Encrypted but not unlocked; refuse to write to disk to avoid leaking plaintext
	}
	if dek == nil {
		return data, nil // Non-encrypted box
	}
	avKey := util.DeriveSubKey(dek, "siyuan/av")
	aad := avAAD(boxID, avID)
	return util.EncryptWithAAD(avKey, data, []byte(aad))
}

func decryptAVData(boxID, avID string, data []byte) ([]byte, error) {
	if AVDEKProvider == nil {
		return data, nil
	}
	if AVLockAcquire != nil {
		AVLockAcquire(boxID)
		defer AVLockRelease(boxID)
	}
	return decryptAVDataLocked(boxID, avID, data)
}

func decryptAVDataLocked(boxID, avID string, data []byte) ([]byte, error) {
	if AVDEKProvider == nil {
		return data, nil
	}
	dek, err := AVDEKProvider(boxID)
	if err != nil {
		return nil, err // Encrypted but not unlocked; refuse to read from disk
	}
	if dek == nil {
		return data, nil // Non-encrypted box
	}
	avKey := util.DeriveSubKey(dek, "siyuan/av")
	aad := avAAD(boxID, avID)
	return util.DecryptWithAAD(avKey, data, []byte(aad))
}

func avAAD(boxID, avID string) string {
	switch avID {
	case "mirror":
		return "siyuan:v1:av-mirror:" + boxID
	case "relation":
		return "siyuan:v1:av-relation:" + boxID
	default:
		return "siyuan:v1:av:" + boxID + ":" + avID
	}
}

// EncryptAVData is the exported version of encryptAVData, for the model layer (import/copy
// database, etc.) to uniformly encrypt AV definitions.
func EncryptAVData(boxID, avID string, data []byte) ([]byte, error) {
	return encryptAVData(boxID, avID, data)
}

// DecryptAVData is the exported version of decryptAVData.
func DecryptAVData(boxID, avID string, data []byte) ([]byte, error) {
	return decryptAVData(boxID, avID, data)
}

// DecryptAVDataLocked decrypts AV data when the caller already holds the corresponding box's read lock.
func DecryptAVDataLocked(boxID, avID string, data []byte) ([]byte, error) {
	return decryptAVDataLocked(boxID, avID, data)
}
