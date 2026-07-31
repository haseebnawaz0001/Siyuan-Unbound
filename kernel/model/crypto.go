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

package model

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/cache"
	"github.com/siyuan-note/siyuan/kernel/conf"
	"github.com/siyuan-note/siyuan/kernel/filesys"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// kekVerifierMagic is the fixed magic value written into KEKVerifier. It is encrypted with the KEK when enabling,
// and decrypted for comparison when verifying the master password.
var kekVerifierMagic = []byte("siyuan-enc-v1")

const boxEncryptionSpec = 1

// errMasterPasswordMigrationPending indicates the global verifier has already been switched during a password change,
// but some notebook configs are still pending recovery.
var errMasterPasswordMigrationPending = errors.New("master password migration is pending")

// notebookCryptoMu serializes control-plane operations on encrypted notebooks (Enable/Disable/Create/ChangeMasterPassword/
// Import/restore, etc.), preventing ChangeMasterPassword's enumeration from racing with CreateEncryptedBox, which could
// otherwise leave a new notebook wrapped with the old KEK after the verifier has already switched -- an unrecoverable state.
var notebookCryptoMu sync.Mutex

// boxLifecycleLocks provides an RWMutex per box to coordinate lock operations with in-flight decryption requests.
// In-flight decryption requests hold the read lock, LockBox holds the write lock, ensuring no new decrypted output is
// produced after locking.
var boxLifecycleLocks = sync.Map{} // map[string]*sync.RWMutex

func acquireBoxReadLock(boxID string) {
	muI, _ := boxLifecycleLocks.LoadOrStore(boxID, &sync.RWMutex{})
	muI.(*sync.RWMutex).RLock()
}

func releaseBoxReadLock(boxID string) {
	if muI, ok := boxLifecycleLocks.Load(boxID); ok {
		muI.(*sync.RWMutex).RUnlock()
	}
}

func acquireBoxWriteLock(boxID string) {
	muI, _ := boxLifecycleLocks.LoadOrStore(boxID, &sync.RWMutex{})
	muI.(*sync.RWMutex).Lock()
}

func releaseBoxWriteLock(boxID string) {
	if muI, ok := boxLifecycleLocks.Load(boxID); ok {
		muI.(*sync.RWMutex).Unlock()
	}
}

// NotebookCryptoMuLock locks notebookCryptoMu, allowing the api layer to read a consistent state snapshot.
func NotebookCryptoMuLock() { notebookCryptoMu.Lock() }

// NotebookCryptoMuUnlock unlocks notebookCryptoMu.
func NotebookCryptoMuUnlock() { notebookCryptoMu.Unlock() }

// notebookCryptoBackupPath is the backup path for NotebookCrypto, located under DataDir/.siyuan/ (within the dejavu sync
// scope).
// MasterSalt is the global root of the encryption system: if conf/conf.json is lost and encryption is re-enabled, a new
// salt is generated, which prevents the old WrappedDEK from being unwrapped with the same master password (the KEK
// changes with the salt). By backing up the entire NotebookCrypto struct to the sync directory, an existing encrypted
// notebook can still be unlocked after conf.json is lost, via sync recovery or the local backup.
// MasterSalt/KEKVerifier are designed to be safe in plaintext (the salt is not secret, and the verifier is ciphertext),
// so the backup file is stored as plaintext JSON.
func notebookCryptoBackupPath() string {
	return filepath.Join(util.DataDir, ".siyuan", "notebook-crypto-backup.json")
}

// computeBackupChecksum computes the SHA-256 checksum of a NotebookCrypto backup.
func computeBackupChecksum(nc *conf.NotebookCrypto) string {
	tmp := *nc
	tmp.Checksum = ""
	tmp.KEKMAC = nil
	data, _ := json.Marshal(tmp)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// computeKEKMAC computes the HMAC-SHA256 authentication code of the backup using the KEK.
func computeKEKMAC(nc *conf.NotebookCrypto, kek []byte) []byte {
	tmp := *nc
	tmp.KEKMAC = nil
	data, _ := json.Marshal(tmp)
	mac := hmac.New(sha256.New, kek)
	mac.Write(data)
	return mac.Sum(nil)
}

// verifyKEKMAC verifies the backup's HMAC-SHA256 authentication code using the KEK.
func verifyKEKMAC(nc *conf.NotebookCrypto, kek []byte) bool {
	if nc == nil || len(nc.KEKMAC) == 0 || len(kek) == 0 {
		return false
	}
	expected := computeKEKMAC(nc, kek)
	return hmac.Equal(expected, nc.KEKMAC)
}

// prepareBackupForWrite prepares the backup metadata fields (Spec/BackupID/CreatedAt/Checksum) before writing.
func prepareBackupForWrite(nc *conf.NotebookCrypto) {
	nc.Spec = conf.CurrentNotebookCryptoSpec
	if nc.BackupID == "" {
		nc.BackupID = util.RandString(16)
	}
	nc.CreatedAt = time.Now().Unix()
	nc.Checksum = computeBackupChecksum(nc)
}

// atomicWriteFile writes atomically: it first writes to a temp file with a random suffix and then renames it, preventing
// partially-written files from being left behind, and avoiding a lost update caused by multiple writers racing on the
// same fixed tmp file name.
func atomicWriteFile(path string, data []byte) error {
	tmpPath := path + "." + gulu.Rand.String(7) + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// ExportNotebookCryptoBackup copies the key backup file into the export directory and returns a downloadable relative
// path.
// This lets the user proactively export and save it, as a recovery path independent of sync (see design doc §4.1).
// The backup file itself does not contain the master password (the salt is not secret and the verifier is ciphertext),
// so obtaining it cannot decrypt any data.
func ExportNotebookCryptoBackup() (downloadPath string, err error) {
	notebookCryptoMu.Lock()
	defer notebookCryptoMu.Unlock()

	backupPath := notebookCryptoBackupPath()
	data, readErr := filelock.ReadFile(backupPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			err = errors.New(Conf.Language(315))
			return
		}
		err = readErr
		return
	}
	exportBase := filepath.Join(util.TempDir, "export")
	if mkErr := os.MkdirAll(exportBase, 0755); mkErr != nil {
		err = mkErr
		return
	}
	// Use a random name to avoid different users/devices overwriting each other; the file name always carries a
	// recognizable prefix
	fileName := "notebook-crypto-backup-" + gulu.Rand.String(7) + ".json"
	downloadPath = "/export/" + url.PathEscape(fileName)
	if writeErr := os.WriteFile(filepath.Join(exportBase, fileName), data, 0644); writeErr != nil {
		err = writeErr
		return
	}
	return
}

// ImportNotebookCryptoBackup accepts the content of a key backup file imported by the user (JSON bytes),
// validates it as a legitimate NotebookCrypto, then writes it back to <DataDir>/.siyuan/notebook-crypto-backup.json and
// loads it into the local Conf.
// Used to manually restore the encryption config on a new device / after reinstalling, without depending on sync (see
// design doc §4.1).
// Security: the backup file does not contain the master password (the salt is not secret and the verifier is
// ciphertext); importing only restores the config, unlocking still requires the master password.
// Guard: import is rejected if encrypted notebooks are already enabled locally, to avoid overwriting the existing
// salt/verifier and orphaning the existing WrappedDEK.
// ImportNotebookCryptoBackup accepts the content of a key backup file imported by the user (JSON bytes) + the master
// password, and only writes the config back after verifying that the master password can unlock the verifier inside the
// backup. This prevents attacks such as a crafted backup that sets weak KDFParams.
// Rejected when already enabled locally (see design §4.1): importing would overwrite the current config with the
// imported backup's MasterSalt/KEKVerifier; if an existing encrypted notebook exists, its WrappedDEK would be orphaned
// by the new KEK (data permanently locked); it is rejected even with no existing notebook, to avoid confusing the user
// once the old master password stops working after being overwritten. Changing key material should go through "disable,
// then import".
func ImportNotebookCryptoBackup(data []byte, password string) error {
	notebookCryptoMu.Lock()
	defer notebookCryptoMu.Unlock()

	// Reject if already enabled (aligned with design §4.1, consistent with the api handler comment)
	Conf.m.RLock()
	enabled := Conf.NotebookCrypto.Enabled
	Conf.m.RUnlock()
	if enabled {
		return errors.New(Conf.Language(324))
	}

	// Reject import if the history directory still holds history from a deleted encrypted notebook: importing would
	// overwrite the current config with the new MasterSalt, but recovering that history still depends on the original
	// MasterSalt, so overwriting it would permanently lock it out (symmetric with the guard in EnableEncryptedNotebook)
	hasHistory, historyErr := scanEncryptedNotebookHistory()
	if historyErr != nil {
		return fmt.Errorf("check encrypted notebook history failed: %w", historyErr)
	}
	if hasHistory {
		return errors.New(Conf.Language(323))
	}

	nc := &conf.NotebookCrypto{}
	if err := json.Unmarshal(data, nc); err != nil {
		return errors.New(Conf.Language(317))
	}
	if len(nc.MasterSalt) == 0 || len(nc.KEKVerifier) == 0 {
		return errors.New(Conf.Language(317))
	}

	// Derive the KEK from the imported salt + the user-entered master password, and verify it can unlock the verifier in
	// the backup
	params, validErr := util.ValidateArgon2Params(nc.KDFParams)
	if validErr != nil {
		return errors.New(Conf.Language(317))
	}
	if nc.Spec != conf.CurrentNotebookCryptoSpec || nc.Checksum == "" || len(nc.KEKMAC) == 0 {
		return errors.New(Conf.Language(317))
	}
	kek := util.DeriveKey(password, nc.MasterSalt, params)
	defer zeroAndClear(kek)
	if nc.Checksum != computeBackupChecksum(nc) {
		return errors.New(Conf.Language(317))
	}
	if !verifyKEKMAC(nc, kek) {
		return errors.New(Conf.Language(317))
	}
	decrypted, dErr := util.DecryptWithAAD(kek, nc.KEKVerifier, []byte("siyuan:v1:kek-verifier"))
	if dErr != nil || string(decrypted) != string(kekVerifierMagic) {
		return errors.New(Conf.Language(311)) // wrong master password
	}

	// If there are already encrypted notebooks, verify the KEK can decrypt their WrappedDEK (prevents importing an old
	// backup with a different salt from locking out existing data)
	if !verifyKEKAgainstExistingBoxes(kek) {
		return errors.New(Conf.Language(316)) // key mismatch
	}

	nc.KDFParams = params // normalize: ensure the params written back to Conf are the validated ones (including default fallback)
	nc.Enabled = true

	// Write the backup first, then commit conf; if the backup write fails, conf hasn't changed yet and the operation can
	// be retried
	if err := writeNotebookCryptoBackupData(nc, kek); err != nil {
		return fmt.Errorf("failed to persist key backup: %w", err)
	}
	Conf.m.Lock()
	*Conf.NotebookCrypto = *nc
	Conf.m.Unlock()
	Conf.Save()
	return nil
}

// saveNotebookCryptoBackup backs up the current NotebookCrypto (including MasterSalt/KEKVerifier/KDFParams) to DataDir.
// kek must be non-nil: the KEKMAC is computed once the Checksum is finalized and persisted, so the recovery path can
// pass MAC verification.
// A backup generated without a KEK would necessarily have an empty KEKMAC, which would be rejected by deriveKEK/the
// recovery path -- effectively creating a state that can never be unlocked (see design §19).
func saveNotebookCryptoBackup(kek []byte) error {
	if kek == nil {
		// A backup in the current format must not be generated without a KEK: a missing KEKMAC would be rejected by
		// deriveKEK/the recovery path, so generating one would be equivalent to creating a state that can never be
		// unlocked.
		return errors.New("cannot generate notebook crypto backup without KEK")
	}
	Conf.m.Lock()
	nc := *Conf.NotebookCrypto // value copy
	prepareBackupForWrite(&nc)
	nc.KEKMAC = computeKEKMAC(&nc, kek)
	Conf.NotebookCrypto.Spec = nc.Spec
	Conf.NotebookCrypto.BackupID = nc.BackupID
	Conf.NotebookCrypto.CreatedAt = nc.CreatedAt
	Conf.NotebookCrypto.Checksum = nc.Checksum
	Conf.NotebookCrypto.KEKMAC = nc.KEKMAC // keep the KEKMAC in Conf consistent with the backup file
	Conf.m.Unlock()
	backupPath := notebookCryptoBackupPath()
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		return fmt.Errorf("mkdir notebook crypto backup dir failed: %w", err)
	}
	data, err := json.Marshal(nc)
	if err != nil {
		return fmt.Errorf("marshal notebook crypto backup failed: %w", err)
	}
	if err := atomicWriteFile(backupPath, data); err != nil {
		return fmt.Errorf("write notebook crypto backup failed: %w", err)
	}
	return nil
}

// writeNotebookCryptoBackupData writes the given NotebookCrypto to the backup file (independent of Conf.NotebookCrypto).
// kek must be non-nil: the KEKMAC is computed once the Checksum is finalized, ensuring the persisted MAC matches the
// persisted content.
func writeNotebookCryptoBackupData(nc *conf.NotebookCrypto, kek []byte) error {
	if kek == nil {
		return errors.New("cannot generate notebook crypto backup without KEK")
	}
	prepareBackupForWrite(nc)
	nc.KEKMAC = computeKEKMAC(nc, kek)
	backupPath := notebookCryptoBackupPath()
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		return fmt.Errorf("mkdir notebook crypto backup dir failed: %w", err)
	}
	data, err := json.Marshal(nc)
	if err != nil {
		return fmt.Errorf("marshal notebook crypto backup failed: %w", err)
	}
	if err := atomicWriteFile(backupPath, data); err != nil {
		return fmt.Errorf("write notebook crypto backup failed: %w", err)
	}
	return nil
}

// verifyKEKAgainstExistingBoxes performs a side-effect-free decryption check of the KEK against the WrappedDEK of every
// existing encrypted notebook.
// It tries the conf's WrappedDEK first, falling back to the backup if decryption fails (consistent with the unlock
// path); it fails closed when GetBoxEncryption errors (an encrypted notebook with corrupted metadata must not be
// silently skipped).
// Returns true if all checks pass or if there are no encrypted notebooks.
func verifyKEKAgainstExistingBoxes(kek []byte) bool {
	boxIDs, err := listAllEncryptedBoxIDs()
	if err != nil {
		logging.LogErrorf("list encrypted notebooks failed: %s", err)
		return false
	}
	for _, id := range boxIDs {
		boxCrypt, err := GetBoxEncryption(id)
		if err != nil {
			return false // metadata read failed -> fail-closed
		}
		if boxCrypt == nil || len(boxCrypt.WrappedDEK) == 0 {
			return false // ListAllEncryptedBoxIDs considers it encrypted but no key material is available -> fail-closed
		}
		if _, dErr := decryptWrappedDEK(id, boxCrypt, kek); dErr == nil {
			continue // decryption succeeded
		}
		// conf's WrappedDEK cannot be decrypted: try the backup (consistent with the unlock path's fallback)
		backup, bErr := readNotebookCryptBackup(id)
		if bErr == nil && backup != nil && len(backup.WrappedDEK) > 0 &&
			!bytes.Equal(backup.WrappedDEK, boxCrypt.WrappedDEK) {
			if _, err2 := decryptWrappedDEK(id, backup, kek); err2 == nil {
				continue // backup decryption succeeded
			}
		}
		return false
	}
	return true
}

// loadNotebookCryptoBackup reads the NotebookCrypto backup from DataDir. Returns (nil, nil) if the file does not exist.
func loadNotebookCryptoBackup() (*conf.NotebookCrypto, error) {
	data, err := filelock.ReadFile(notebookCryptoBackupPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	nc := &conf.NotebookCrypto{}
	if err := json.Unmarshal(data, nc); err != nil {
		return nil, err
	}
	conf.UpgradeSpec(nc)
	if nc.Spec != conf.CurrentNotebookCryptoSpec {
		return nil, fmt.Errorf("unsupported notebook crypto backup spec [%d]", nc.Spec)
	}
	if nc.Checksum == "" {
		return nil, errors.New("notebook crypto backup checksum is missing")
	}
	expected := computeBackupChecksum(nc)
	if nc.Checksum != expected {
		logging.LogWarnf("notebook crypto backup checksum mismatch: expected %s, got %s", expected, nc.Checksum)
		return nil, errors.New("notebook crypto backup is corrupted (checksum mismatch)")
	}
	return nc, nil
}

// removeNotebookCryptoBackup deletes the backup file (called when disabling the encryption feature). A missing file is
// treated as success.
func removeNotebookCryptoBackup() {
	if err := os.Remove(notebookCryptoBackupPath()); err != nil && !os.IsNotExist(err) {
		logging.LogErrorf("remove notebook crypto backup failed: %s", err)
	}
}

// masterPasswordMigration records the complete state of a master password migration, used for recovery after a crash.
type masterPasswordMigration struct {
	OldVerifier      []byte              `json:"oldVerifier"`
	NewVerifier      []byte              `json:"newVerifier"`
	NewVerifierNonce []byte              `json:"newVerifierNonce"`
	NewKDFParams     json.RawMessage     `json:"newKDFParams"`
	Boxes            []migrationBoxEntry `json:"boxes"`
}

type migrationBoxEntry struct {
	BoxID         string `json:"boxID"`
	NewSpec       int    `json:"newSpec"`
	NewWrappedDEK []byte `json:"newWrappedDEK"`
	NewWrapNonce  []byte `json:"newWrapNonce"`
}

func masterPasswordMigrationPath() string {
	return filepath.Join(util.DataDir, ".siyuan", "master-password-migration.json")
}

func writeMasterPasswordMigration(m *masterPasswordMigration) error {
	p := masterPasswordMigrationPath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("mkdir master password migration dir failed: %w", err)
	}
	data, err := gulu.JSON.MarshalIndentJSON(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal master password migration failed: %w", err)
	}
	return filelock.WriteFile(p, data)
}

func readMasterPasswordMigration() (*masterPasswordMigration, error) {
	p := masterPasswordMigrationPath()
	if !filelock.IsExist(p) {
		return nil, nil
	}
	data, err := filelock.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read master password migration failed: %w", err)
	}
	var m masterPasswordMigration
	if err = gulu.JSON.UnmarshalJSON(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshal master password migration failed: %w", err)
	}
	return &m, nil
}

func removeMasterPasswordMigration() {
	p := masterPasswordMigrationPath()
	if err := filelock.Remove(p); err != nil && !os.IsNotExist(err) {
		logging.LogErrorf("remove master password migration failed: %s", err)
	}
}

// MasterPasswordMigrationStatus returns whether a pending master password migration exists and the affected notebooks.
func MasterPasswordMigrationStatus() (pending bool, boxIDs []string) {
	mig, err := readMasterPasswordMigration()
	if err != nil || mig == nil {
		return false, nil
	}
	for _, entry := range mig.Boxes {
		boxIDs = append(boxIDs, entry.BoxID)
	}
	return true, boxIDs
}

// recoverMasterPasswordMigration detects and completes an interrupted master password migration at startup.
// If the migration manifest exists, the recovery strategy is decided based on whether the global verifier has already
// been switched.
func recoverMasterPasswordMigration() {
	mig, err := readMasterPasswordMigration()
	if err != nil {
		logging.LogErrorf("read master password migration failed: %s", err)
		return
	}
	if mig == nil {
		return // no pending migration to recover
	}

	Conf.m.RLock()
	currentVerifier := Conf.NotebookCrypto.KEKVerifier
	Conf.m.RUnlock()

	if bytes.Equal(currentVerifier, mig.NewVerifier) {
		// Phase 2 already completed (verifier switched); backfill any unfinished boxes
		for _, entry := range mig.Boxes {
			box := &Box{ID: entry.BoxID}
			boxConf := box.GetConf()
			if !boxConf.Encrypted || boxConf.BoxCrypt == nil {
				// conf missing/corrupted: try to rebuild from the per-notebook backup
				backup, bErr := readNotebookCryptBackup(entry.BoxID)
				if bErr == nil && backup != nil && len(backup.WrappedDEK) > 0 {
					boxConf = box.GetConf() // re-fetch the default conf
					boxConf.Encrypted = true
					boxConf.BoxCrypt = backup
					if saveErr := box.SaveConf(boxConf); saveErr != nil {
						logging.LogErrorf("rebuild encrypted conf from backup [%s] failed: %s", entry.BoxID, saveErr)
						return // keep the manifest
					}
				} else {
					// Neither conf nor backup is available: the manifest is the authoritative source for this box's
					// encryption key (NewWrappedDEK/NewWrapNonce/NewSpec), so rebuild BoxCrypt directly from the manifest,
					// avoiding a permanent failure loop when both conf and backup are missing. boxConf's non-encryption
					// metadata (e.g. Name) has already been lost along with conf at this point and falls back to default
					// values, but the box's document tree (.sy files) is unaffected, so data reachability is preserved.
					logging.LogWarnf("rebuild encrypted box [%s] from migration manifest (conf and backup both unavailable)", entry.BoxID)
					boxConf = box.GetConf()
					boxConf.Encrypted = true
					boxConf.BoxCrypt = &conf.BoxEncryption{
						WrappedDEK: entry.NewWrappedDEK,
						WrapNonce:  entry.NewWrapNonce,
						Spec:       entry.NewSpec,
						CreatedAt:  time.Now().UnixMilli(),
					}
					if saveErr := box.SaveConf(boxConf); saveErr != nil {
						logging.LogErrorf("rebuild encrypted conf from manifest [%s] failed: %s", entry.BoxID, saveErr)
						return // keep the manifest
					}
				}
			}
			// If WrappedDEK already matches, skip writing conf, but still make sure the per-notebook backup is up to date
			if bytes.Equal(boxConf.BoxCrypt.WrappedDEK, entry.NewWrappedDEK) {
				if writeErr := writeNotebookCryptBackup(entry.BoxID, boxConf.BoxCrypt); writeErr != nil {
					logging.LogErrorf("refresh box crypt backup [%s] failed: %s", entry.BoxID, writeErr)
					return // keep the manifest
				}
				continue
			}
			boxConf.BoxCrypt.WrappedDEK = entry.NewWrappedDEK
			boxConf.BoxCrypt.Spec = entry.NewSpec
			boxConf.BoxCrypt.WrapNonce = entry.NewWrapNonce
			if saveErr := box.SaveConf(boxConf); saveErr != nil {
				logging.LogErrorf("recover box conf [%s] failed: %s", entry.BoxID, saveErr)
				return // keep the manifest
			}
			if writeErr := writeNotebookCryptBackup(entry.BoxID, boxConf.BoxCrypt); writeErr != nil {
				logging.LogErrorf("recover box crypt backup [%s] failed: %s", entry.BoxID, writeErr)
				return // keep the manifest
			}
		}
		// Persist the global conf. At this point there is no KEK derived from the new password, so a trustworthy MAC
		// cannot be generated for a new backup; therefore keep the migration manifest and the old backup, and only
		// complete the backup switch once the user enters the new password for the first time and all WrappedDEKs are
		// verified.
		Conf.Save()
		logging.LogInfof("master password migration data recovered, waiting for the new password to authenticate the backup")
	} else {
		// Phase 2 not completed: clear the manifest, keep the old verifier + old WrappedDEK, state remains consistent
		removeMasterPasswordMigration()
		logging.LogErrorf("master password migration was interrupted, please retry")
	}
}

// hasEncryptedNotebook checks whether an encrypted notebook exists in the data directory, independent of whether the
// global encryption feature is enabled.
// EnableEncryptedNotebook uses it to avoid regenerating MasterSalt, which would orphan the old WrappedDEK.
func hasEncryptedNotebook() (bool, error) {
	ids, err := listAllEncryptedBoxIDs()
	return len(ids) > 0, err
}

// HasEncryptedNotebookHistory checks whether an encrypted notebook's history snapshot exists in the history directory.
// After a notebook is deleted, its box directory (including .siyuan/conf.json and notebook-crypt-backup.json) is backed
// up as-is (still ciphertext) to the history directory (via RemoveBox's filelock.Copy), but by then IsEncryptedBox
// already returns false (the box directory has been removed). So DisableEncryptedNotebook cannot rely solely on
// ListAllEncryptedBoxIDs -- the history of a deleted encrypted notebook still depends on the current
// MasterSalt/KEKVerifier to be recoverable, and disabling encryption and deleting the backup would permanently lock out
// that history, violating design §19. This function scans the history directory to detect such dependencies.
//
// Detection signal: a history entry <HistoryDir>/<ts>-<op>/<boxID>/.siyuan/ contains notebook-crypt-backup.json
// (specifically designed for recovery after box deletion), or conf.json marks Encrypted=true.
// boxID is validated with ast.IsNodeIDPattern, to avoid misidentifying non-box directories such as assets/storage.
func scanEncryptedNotebookHistory() (bool, error) {
	entries, err := os.ReadDir(util.HistoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read history dir failed: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// History snapshot directory: <ts>-<op>, containing per-boxID subdirectories
		snapshotDir := filepath.Join(util.HistoryDir, entry.Name())
		boxEntries, readErr := os.ReadDir(snapshotDir)
		if readErr != nil {
			return false, fmt.Errorf("read history snapshot [%s] failed: %w", entry.Name(), readErr)
		}
		for _, boxEntry := range boxEntries {
			if !boxEntry.IsDir() || !ast.IsNodeIDPattern(boxEntry.Name()) {
				continue
			}
			encrypted, checkErr := isEncryptedHistoryBoxDir(filepath.Join(snapshotDir, boxEntry.Name()))
			if checkErr != nil {
				return false, checkErr
			}
			if encrypted {
				return true, nil
			}
		}
	}
	return false, nil
}

// HasEncryptedNotebookHistory treats a scan failure as if a dependency exists, so callers don't delete recovery
// material because of an I/O or permission error.
func HasEncryptedNotebookHistory() bool {
	hasHistory, err := scanEncryptedNotebookHistory()
	if err != nil {
		logging.LogErrorf("scan encrypted notebook history failed: %s", err)
		return true
	}
	return hasHistory
}

// isEncryptedHistoryBoxDir determines whether a boxID subdirectory in the history directory belongs to an encrypted
// notebook.
// It checks notebook-crypt-backup.json first (backed up together with the box directory before deletion, the
// authoritative marker of encrypted identity), then falls back to the Encrypted flag in conf.json.
func isEncryptedHistoryBoxDir(boxDir string) (bool, error) {
	siyuanDir := filepath.Join(boxDir, ".siyuan")
	backupPath := filepath.Join(siyuanDir, "notebook-crypt-backup.json")
	if _, err := os.Stat(backupPath); err == nil {
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat encrypted notebook history backup [%s] failed: %w", boxDir, err)
	}
	confPath := filepath.Join(siyuanDir, "conf.json")
	if _, err := os.Stat(confPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat encrypted notebook history conf [%s] failed: %w", boxDir, err)
	}
	data, err := filelock.ReadFile(confPath)
	if err != nil {
		return false, fmt.Errorf("read encrypted notebook history conf [%s] failed: %w", boxDir, err)
	}
	var boxConf conf.BoxConf
	if err = gulu.JSON.UnmarshalJSON(data, &boxConf); err != nil {
		return false, fmt.Errorf("parse encrypted notebook history conf [%s] failed: %w", boxDir, err)
	}
	return boxConf.Encrypted, nil
}

// cachedDEKs caches the DEKs of unlocked encrypted notebooks, indexed by boxID.
// The KEK is never cached globally (enforcing "each notebook is unlocked strictly independently" semantics): UnlockBox
// derives the KEK temporarily to decrypt the DEK, then discards the KEK immediately, keeping only the per-box DEK for
// subsequent read/write encryption and decryption.
var (
	cachedDEKs     = map[string][]byte{}
	cachedDEKsLock sync.RWMutex
)

// boxLastAccess records, per encrypted notebook, the time of the most recent real user interaction or explicit
// keep-alive (unix nanoseconds), used by the auto-lock cron job.
// key: boxID, value: *atomic.Int64. Initialized when UnlockBox succeeds, cleared on Unmount.
var boxLastAccess sync.Map

// EnableEncryptedNotebook enables the encrypted notebook feature: generates a MasterSalt, derives a KEK, writes the
// verifier value, and persists it.
// Calling it again while already enabled returns an error, to avoid overwriting an existing encrypted notebook's key
// parameters.
// The KEK is not cached -- after enabling, the user needs to call UnlockBox separately for each encrypted notebook to
// unlock it.
func EnableEncryptedNotebook(password string) error {
	if len(password) == 0 {
		return errors.New("password must not be empty")
	}

	notebookCryptoMu.Lock()
	defer notebookCryptoMu.Unlock()

	Conf.m.Lock()
	if Conf.NotebookCrypto.Enabled {
		Conf.m.Unlock()
		return errors.New(Conf.Language(312))
	}
	Conf.m.Unlock()

	// Guard: if an encrypted notebook already exists (a box with Encrypted=true on disk), MasterSalt must not be
	// regenerated.
	// Re-enabling after conf.json is lost would derive a new KEK, so the old WrappedDEK could no longer be unwrapped
	// even with the same master password (data permanently locked).
	// In that case the original MasterSalt/KEKVerifier must be restored from the DataDir backup, and recovery only
	// counts as successful once the master password is verified against it.
	hasEncrypted, listErr := hasEncryptedNotebook()
	if listErr != nil {
		return fmt.Errorf("list encrypted notebooks failed: %w", listErr)
	}
	if hasEncrypted {
		if _, restoreErr := tryRestoreNotebookCryptoFromBackupLocked(password); restoreErr != nil {
			// Restore failed: if it's a wrong master password (the restore function returns the 311 message), keep the
			// original message; otherwise (backup missing/corrupted) prompt the user to restore the backup
			if strings.Contains(restoreErr.Error(), Conf.Language(311)) {
				return errors.New(Conf.Language(311)) // wrong master password
			}
			return errors.New(Conf.Language(315))
		}
		logging.LogInfof("encrypted notebook re-enabled with restored master key material from backup")
		return nil
	}

	// Guard: if a history snapshot of a deleted encrypted notebook exists in the history directory, MasterSalt must not
	// be regenerated.
	// Recovering that history still depends on the original MasterSalt/KEKVerifier; generating a new salt would
	// permanently lock it out (violating design §4.1, "regenerating MasterSalt is forbidden while kernel-enumerable
	// encryption recovery data exists"). In that case, restore the global key backup or clear the history first.
	hasHistory, historyErr := scanEncryptedNotebookHistory()
	if historyErr != nil {
		return fmt.Errorf("check encrypted notebook history failed: %w", historyErr)
	}
	if hasHistory {
		return errors.New(Conf.Language(323))
	}

	// No encrypted notebook and no history dependency: generate a new MasterSalt normally
	salt, err := util.GenerateSalt()
	if err != nil {
		return err
	}
	Conf.m.RLock()
	kdfParams := Conf.NotebookCrypto.KDFParams
	Conf.m.RUnlock()
	params, validErr := util.ValidateArgon2Params(kdfParams)
	if validErr != nil {
		return validErr
	}
	kek := util.DeriveKey(password, salt, params)
	defer zeroAndClear(kek)

	// Encrypt the fixed magic value with the KEK as the verifier; once persisted, it lets subsequent UnlockBox calls
	// verify offline
	verifierCT, err := util.EncryptWithAAD(kek, kekVerifierMagic, []byte("siyuan:v1:kek-verifier"))
	if err != nil {
		return err
	}
	verifierNonce, nonceErr := util.EncryptionNonce(verifierCT)
	if nonceErr != nil {
		return nonceErr
	}

	Conf.m.Lock()
	previous := *Conf.NotebookCrypto
	Conf.NotebookCrypto.Enabled = true
	Conf.NotebookCrypto.MasterSalt = salt
	Conf.NotebookCrypto.KDFParams = params
	Conf.NotebookCrypto.KEKVerifier = verifierCT
	Conf.NotebookCrypto.VerifierNonce = verifierNonce
	Conf.m.Unlock()

	// Persist the recovery backup first, then commit conf. At this point there is no encrypted notebook or history
	// dependency yet, so a failure at either step cannot orphan any existing ciphertext.
	if err := saveNotebookCryptoBackup(kek); err != nil {
		// If the backup write fails, restore the pre-enable in-memory config; conf hasn't been written yet, so no disk
		// rollback is needed.
		logging.LogErrorf("save notebook crypto backup failed: %s", err)
		Conf.m.Lock()
		*Conf.NotebookCrypto = previous
		Conf.m.Unlock()
		return fmt.Errorf("enable encrypted notebook failed: failed to persist key backup: %w", err)
	}
	// Conf.Save locks Conf.m internally, so it must not be called while already holding the lock (RWMutex is not
	// reentrant).
	// Even if the config write fails, the already-persisted backup can still restore the same key material on next
	// startup.
	Conf.Save()
	return nil
}

// DisableEncryptedNotebook turns off the encrypted notebook feature. Precondition: no encrypted notebooks may exist,
// and there must be no deleted-notebook history that still depends on the current key backup (otherwise disabling and
// deleting the backup would permanently lock out that history, violating §19).
// Clears the global encryption config (MasterSalt/KEKVerifier); the KEK/DEK become unavailable.
func DisableEncryptedNotebook() error {
	notebookCryptoMu.Lock()
	defer notebookCryptoMu.Unlock()

	// Check whether any encrypted notebook still exists (including ones with corrupted conf but an existing backup)
	ids, listErr := listAllEncryptedBoxIDs()
	if listErr != nil {
		return fmt.Errorf("list encrypted notebooks failed: %w", listErr)
	}
	if len(ids) > 0 {
		return errors.New("cannot disable encrypted notebook feature while encrypted notebooks exist, remove them first")
	}
	// Check whether the history directory holds a history snapshot from a deleted encrypted notebook: recovering it
	// still depends on the current MasterSalt/KEKVerifier, so this history must be cleared before deleting the backup
	// (see design §19)
	hasHistory, historyErr := scanEncryptedNotebookHistory()
	if historyErr != nil {
		return fmt.Errorf("check encrypted notebook history failed: %w", historyErr)
	}
	if hasHistory {
		return errors.New(Conf.Language(323))
	}

	Conf.m.Lock()
	Conf.NotebookCrypto.Enabled = false
	Conf.NotebookCrypto.MasterSalt = nil
	Conf.NotebookCrypto.KEKVerifier = nil
	Conf.NotebookCrypto.VerifierNonce = nil
	Conf.m.Unlock()

	Conf.Save()
	removeNotebookCryptoBackup() // clean up the backup when disabling, to avoid leaving stale key material behind
	return nil
}

// restoreNotebookCryptoConfigFromBackup loads the NotebookCrypto config from the backup back into the local conf.json
// (no master password needed).
// Used after data sync / importing a Data.zip: the backup file arrives with DataDir on the new device, but the local
// conf.json's NotebookCrypto is still empty. This loads the salt/verifier/KDFParams back and sets Enabled=true, so the
// UI shows "enabled" and notebooks show as locked (unlocking still requires the master password).
// Precondition: only called when Enabled=false locally, to avoid overwriting the local config that's currently in use.
// Security: the salt is not secret and the verifier is ciphertext, so loading the config back does not expose any
// plaintext data (unlocking still requires deriving the KEK from the master password).
func restoreNotebookCryptoConfigFromBackup() {
	notebookCryptoMu.Lock()
	defer notebookCryptoMu.Unlock()

	Conf.m.RLock()
	enabled := Conf.NotebookCrypto.Enabled
	Conf.m.RUnlock()
	if enabled {
		return // already enabled locally, don't overwrite
	}
	backup, err := loadNotebookCryptoBackup()
	if err != nil || backup == nil || len(backup.MasterSalt) == 0 || len(backup.KEKVerifier) == 0 {
		return // no usable backup available, silently skip
	}
	// Validate KDFParams: reject recovery on invalid params
	params, validErr := util.ValidateArgon2Params(backup.KDFParams)
	if validErr != nil {
		logging.LogErrorf("skip restore notebook crypto: invalid KDFParams in backup: %s", validErr)
		return
	}
	backup.KDFParams = params

	backup.Enabled = true
	Conf.m.Lock()
	*Conf.NotebookCrypto = *backup
	Conf.m.Unlock()
	Conf.Save()
	logging.LogInfof("notebook crypto config restored from backup (auto-enable after sync/import)")
}

// tryRestoreNotebookCryptoFromBackupLocked attempts to restore from the DataDir backup when the local NotebookCrypto is
// not enabled.
// After data syncs to a new device, the local conf.json's NotebookCrypto is empty (Enabled=false), but the backup file
// has already synced along with DataDir. When the user then clicks an encrypted notebook and enters the master
// password, deriveKEK calls this function to verify the master password against the verifier in the backup; once
// verified, it loads the salt/verifier back and sets Enabled=true, letting the old WrappedDEK be unwrapped normally.
// On successful verification it also returns the already-derived KEK (the salt used for recovery is the same as the
// one loaded back, so deriveKEK doesn't need to run Argon2id again).
// A returned error means recovery failed (backup missing / wrong master password), in which case KEK is nil.
func tryRestoreNotebookCryptoFromBackupLocked(password string) (kek []byte, err error) {
	backup, bErr := loadNotebookCryptoBackup()
	if bErr != nil || backup == nil || len(backup.MasterSalt) == 0 || len(backup.KEKVerifier) == 0 {
		// backup missing or incomplete: cannot recover, the caller reports the error as "not enabled"
		return nil, errors.New(Conf.Language(310))
	}
	params, validErr := util.ValidateArgon2Params(backup.KDFParams)
	if validErr != nil {
		return nil, errors.New(Conf.Language(317))
	}
	kek = util.DeriveKey(password, backup.MasterSalt, params)
	decrypted, dErr := util.DecryptWithAAD(kek, backup.KEKVerifier, []byte("siyuan:v1:kek-verifier"))
	if dErr != nil || string(decrypted) != string(kekVerifierMagic) {
		// wrong master password (or corrupted backup), cannot recover
		zeroAndClear(kek)
		return nil, errors.New(Conf.Language(311))
	}
	// A backup in the current format must carry a valid KEKMAC; both a missing and a mismatched KEKMAC are treated as
	// tampering.
	if backup.Spec != conf.CurrentNotebookCryptoSpec || backup.Checksum == "" ||
		len(backup.KEKMAC) == 0 || !verifyKEKMAC(backup, kek) {
		zeroAndClear(kek)
		return nil, errors.New(Conf.Language(316))
	}

	// If any encrypted notebook exists, verify the KEK can decrypt its WrappedDEK (prevents a backup with a mismatched
	// salt from locking out data)
	if !verifyKEKAgainstExistingBoxes(kek) {
		zeroAndClear(kek)
		return nil, errors.New(Conf.Language(316)) // key mismatch
	}

	backup.KDFParams = params // normalize: ensure the params written back to Conf are the validated ones (including default fallback)
	backup.Enabled = true
	Conf.m.Lock()
	*Conf.NotebookCrypto = *backup
	Conf.m.Unlock()
	Conf.Save()
	// After a successful restore, synchronously rewrite the backup (filling in the KEKMAC, upgrading an old backup to
	// Spec=1).
	// The caller already holds notebookCryptoMu, and writeNotebookCryptoBackupData doesn't acquire that lock again, so
	// there's no deadlock; writing synchronously avoids racing on the same file with a concurrent backup write from
	// ChangeMasterPassword (a lost update could roll back the verifier).
	nc := *backup
	if err := writeNotebookCryptoBackupData(&nc, kek); err != nil {
		logging.LogWarnf("rewrite notebook crypto backup after restore failed: %s", err)
	}
	logging.LogInfof("notebook crypto restored from backup (e.g. after sync to a new device)")
	return kek, nil
}

// deriveKEK derives the KEK from the master password and verifies it. Returns an error on verification failure. The KEK
// is only valid within the function's scope; the caller is responsible for its use.
func deriveKEK(password string) ([]byte, error) {
	Conf.m.RLock()
	nc := *Conf.NotebookCrypto
	Conf.m.RUnlock()

	if !nc.Enabled {
		// Not enabled locally: this may be because data synced to a new device and the local conf.json doesn't have the
		// encryption config yet.
		// Try to restore from the DataDir backup (the backup syncs along with DataDir); on success, reuse the KEK it
		// already derived.
		kek, restoreErr := tryRestoreNotebookCryptoFromBackupLocked(password)
		if restoreErr != nil {
			return nil, restoreErr
		}
		return kek, nil // the restore function has already verified the verifier, so the KEK is usable directly
	}
	params, validErr := util.ValidateArgon2Params(nc.KDFParams)
	if validErr != nil {
		return nil, validErr
	}
	kek := util.DeriveKey(password, nc.MasterSalt, params)

	decrypted, err := util.DecryptWithAAD(kek, nc.KEKVerifier, []byte("siyuan:v1:kek-verifier"))
	if err != nil {
		zeroAndClear(kek)
		return nil, errors.New(Conf.Language(311))
	}
	if string(decrypted) != string(kekVerifierMagic) {
		zeroAndClear(kek)
		return nil, errors.New(Conf.Language(311))
	}

	// A normal config must pass KEKMAC authentication; a missing MAC must never be treated as a compatibility path,
	// otherwise an attacker on the sync side could delete the MAC, recompute the keyless Checksum, and tamper with
	// security-relevant config such as AutoLockMinutes.
	mig, migErr := readMasterPasswordMigration()
	migrationPending := migErr == nil && mig != nil && bytes.Equal(nc.KEKVerifier, mig.NewVerifier)
	if !migrationPending {
		backup, backupErr := loadNotebookCryptoBackup()
		backupMatchesConf := backupErr == nil && backup != nil &&
			bytes.Equal(backup.MasterSalt, nc.MasterSalt) &&
			bytes.Equal(backup.KEKVerifier, nc.KEKVerifier) &&
			backup.KDFParams == nc.KDFParams
		if !backupMatchesConf || backup.Spec != conf.CurrentNotebookCryptoSpec || backup.Checksum == "" ||
			len(backup.KEKMAC) == 0 || !verifyKEKMAC(backup, kek) {
			// The master password has already passed the local verifier check (reaching here means kek is correct), but
			// the backup is missing or the KEKMAC is invalid: this is an incomplete config rather than a wrong password
			// or corrupted key, so guide the user to import a matching backup file to recover (Language 315).
			zeroAndClear(kek)
			return nil, errors.New(Conf.Language(315))
		}
	}

	if migrationPending {
		// First new-password verification after crash recovery: confirm all notebooks have switched to the new KEK,
		// then generate an authenticated global backup and end the migration.
		if !verifyKEKAgainstExistingBoxes(kek) {
			zeroAndClear(kek)
			return nil, errMasterPasswordMigrationPending
		}
		if err = saveNotebookCryptoBackup(kek); err != nil {
			zeroAndClear(kek)
			return nil, fmt.Errorf("%w: %v", errMasterPasswordMigrationPending, err)
		}
		removeMasterPasswordMigration()
	}
	return kek, nil
}

// decryptBoxCrypt decrypts a box's WrappedDEK with the KEK. It prefers the result of GetBoxEncryption (conf -> backup
// fallback), and if decryption fails, tries the different WrappedDEK found in the backup.
// Returns the decrypted DEK and the BoxCrypt actually used (which may come from the backup).
// If the backup ends up being used, it automatically repairs conf.json and refreshes the backup.
func decryptBoxCrypt(boxID string, kek []byte) (dek []byte, boxCrypt *conf.BoxEncryption, err error) {
	boxCrypt, err = GetBoxEncryption(boxID)
	if err != nil || boxCrypt == nil || len(boxCrypt.WrappedDEK) == 0 {
		return nil, nil, fmt.Errorf("no encrypted key material for box [%s]", boxID)
	}

	dek, err = decryptWrappedDEK(boxID, boxCrypt, kek)
	if err == nil {
		return dek, boxCrypt, nil
	}

	// The primary BoxCrypt cannot be decrypted: try the different WrappedDEK in the backup
	backup, bErr := readNotebookCryptBackup(boxID)
	if bErr == nil && backup != nil && len(backup.WrappedDEK) > 0 &&
		!bytes.Equal(backup.WrappedDEK, boxCrypt.WrappedDEK) {
		dek, err = decryptWrappedDEK(boxID, backup, kek)
		if err == nil {
			// backup decryption succeeded: repair conf + refresh the backup
			box := &Box{ID: boxID}
			boxConf := box.GetConf()
			boxConf.Encrypted = true
			boxConf.BoxCrypt = backup
			if saveErr := box.SaveConf(boxConf); saveErr != nil {
				logging.LogWarnf("fix encrypted box conf from backup [%s] failed: %s", boxID, saveErr)
			}
			if needWriteNotebookCryptBackup(boxID, backup) {
				if writeErr := writeNotebookCryptBackup(boxID, backup); writeErr != nil {
					logging.LogWarnf("refresh notebook crypt backup [%s] failed: %s", boxID, writeErr)
				}
			}
			return dek, backup, nil
		}
	}
	return nil, nil, fmt.Errorf("decrypt box [%s] failed: incorrect key or corrupted data", boxID)
}

// UnlockBox derives the KEK from the master password, decrypts the notebook's DEK, and caches it. The KEK is discarded
// right after use and is never cached globally.
// Every call runs Argon2id once (roughly 1 second), strictly enforcing the "each notebook unlocked independently"
// semantics.
func UnlockBox(boxID string, password string, boxEnc *conf.BoxEncryption) error {
	if boxEnc == nil || len(boxEnc.WrappedDEK) == 0 {
		return errors.New("no encrypted key material for box")
	}

	// The global config lock is acquired before the notebook lifecycle lock (design §17 lock-ordering convention), to
	// avoid a deadlock with a path that holds a subsystem lock and then tries to acquire the config lock.
	// While notebookCryptoMu is held, deriveKEK/conf repair only acquire Conf.m/cachedDEKsLock, and never re-acquire the
	// box lifecycle lock.
	notebookCryptoMu.Lock()
	defer notebookCryptoMu.Unlock()

	// Acquire the box write lock, serializing with LockBox/unmount0, to prevent concurrent lock/unlock from causing
	// db/DEK state inconsistency
	acquireBoxWriteLock(boxID)
	defer releaseBoxWriteLock(boxID)

	kek, err := deriveKEK(password)
	if err != nil {
		return err
	}
	defer zeroAndClear(kek)

	// Use decryptBoxCrypt to uniformly handle decryption + backup fallback + conf repair
	dek, trustedCrypt, err := decryptBoxCrypt(boxID, kek)
	if err != nil {
		return errors.New(Conf.Language(316))
	}
	boxEnc = trustedCrypt

	// Hold the lock to protect the atomicity of "open db + cache DEK", avoiding db/DEK inconsistency with a concurrent
	// LockBox
	cachedDEKsLock.Lock()
	defer cachedDEKsLock.Unlock()
	if err = sql.OpenEncryptedDB(boxID, dek); err != nil {
		return err
	}
	if err = treenode.OpenEncryptedBlockTreeDB(boxID, dek); err != nil {
		sql.RemoveEncryptedDBFile(boxID) // clean up the already-created content db file, avoiding leaving behind an empty encrypted database
		return err
	}
	cachedDEKs[boxID] = dek

	// Initialize the auto-lock access timestamp, recording the unlock moment
	newVal := &atomic.Int64{}
	newVal.Store(time.Now().UnixNano())
	boxLastAccess.Store(boxID, newVal)

	// Repair conf.json: fix it if conf doesn't correctly mark the encryption state (e.g. after unlocking from the
	// backup)
	box := &Box{ID: boxID}
	boxConf := box.GetConf()
	if boxConf == nil || !boxConf.Encrypted || boxConf.BoxCrypt == nil ||
		len(boxConf.BoxCrypt.WrappedDEK) == 0 ||
		!bytes.Equal(boxConf.BoxCrypt.WrappedDEK, boxEnc.WrappedDEK) {
		boxConf.Encrypted = true
		boxConf.BoxCrypt = boxEnc
		if saveErr := box.SaveConf(boxConf); saveErr != nil {
			logging.LogWarnf("fix encrypted box conf [%s] failed: %s", boxID, saveErr)
		}
	}

	// Refresh the per-notebook backup (when missing or inconsistent), to aid recovery if conf gets corrupted
	if needWriteNotebookCryptBackup(boxID, boxEnc) {
		if err = writeNotebookCryptBackup(boxID, boxEnc); err != nil {
			logging.LogWarnf("write notebook crypt backup [%s] failed: %s", boxID, err)
		}
	}
	return nil
}

// IsBoxUnlocked returns whether the notebook's DEK is in memory (i.e. whether it is unlocked).
func IsBoxUnlocked(boxID string) bool {
	cachedDEKsLock.RLock()
	defer cachedDEKsLock.RUnlock()
	_, ok := cachedDEKs[boxID]
	return ok
}

// LockBox clears the given notebook's DEK and removes its encrypted db files. Called when unmounting a single
// encrypted notebook or locking it manually.
func LockBox(boxID string) {
	FlushTxQueue()
	acquireBoxWriteLock(boxID)
	lockBoxHeld(boxID)
	releaseBoxWriteLock(boxID)
	// After locking a single box, the global caches (tree/Block/IAL/AV) need to be refreshed
	cache.ClearTreeCache()
	sql.ClearCache()
	cache.ClearDocsIAL()
	cache.ClearBlocksIAL()
	cache.ClearAVCache()
	ResetVirtualBlockRefCache()
}

// lockBoxHeld performs the lock cleanup for the box assuming its write lock is already held (excluding global cache
// refresh).
func lockBoxHeld(boxID string) {
	RevokeManagedEncryptedExportsForBox(boxID)

	cachedDEKsLock.Lock()
	if dek, ok := cachedDEKs[boxID]; ok {
		zeroAndClear(dek)
		delete(cachedDEKs, boxID)
	}
	cachedDEKsLock.Unlock()

	// Clean up the auto-lock access timestamp
	boxLastAccess.Delete(boxID)

	// Only backfill from conf when the backup is missing. In the normal flow, CreateEncryptedBox/UnlockBox/
	// ChangeMasterPassword have already refreshed the backup, so an existing backup is never overwritten here with a
	// BoxCrypt that hasn't been decryption-verified,
	// preventing a bad WrappedDEK in conf from overwriting a valid recovery source.
	if !filelock.IsExist(notebookCryptBackupPath(boxID)) {
		box := &Box{ID: boxID}
		boxConf := box.GetConf()
		if boxConf != nil && boxConf.Encrypted && boxConf.BoxCrypt != nil && len(boxConf.BoxCrypt.WrappedDEK) > 0 {
			if err := writeNotebookCryptBackup(boxID, boxConf.BoxCrypt); err != nil {
				logging.LogWarnf("write notebook crypt backup [%s] failed: %s", boxID, err)
			}
		}
	}

	sql.RemoveEncryptedDBFile(boxID)
	treenode.RemoveEncryptedBlockTreeDBFile(boxID)
	// Clean up the decrypted files for this encrypted box in the repo temp directory (diff/rollback/sync conflicts)
	repoDirs := []string{
		filepath.Join(util.TempDir, "repo", "diff", boxID),
		filepath.Join(util.TempDir, "repo", "rollback", boxID),
	}
	for _, d := range repoDirs {
		if rmErr := os.RemoveAll(d); rmErr != nil {
			logging.LogWarnf("remove repo dir for box [%s] failed: %s", boxID, rmErr)
		}
	}
	// The sync/conflicts path has a timestamp prefix, matched with a glob
	if matches, globErr := filepath.Glob(filepath.Join(util.TempDir, "repo", "sync", "conflicts", "*", boxID)); globErr == nil {
		for _, m := range matches {
			if rmErr := os.RemoveAll(m); rmErr != nil {
				logging.LogWarnf("remove repo sync conflict dir for box [%s] failed: %s", boxID, rmErr)
			}
		}
	}
	// Clean up the temporary exports for this encrypted box in the temp export directory (htmlmd/html/PDF)
	if rmErr := os.RemoveAll(filepath.Join(util.TempDir, "export", boxID)); rmErr != nil {
		logging.LogWarnf("remove export/[%s] dir failed: %s", boxID, rmErr)
	}
	// Clean up the dynamic reference anchor text cache
	treenode.RemoveDynamicRefTexts(boxID)
}

// WrapNewDEK generates a random DEK and wraps it with the given KEK, returning the BoxEncryption metadata.
// The KEK is derived temporarily by the caller (not from the global cache); the caller is responsible for discarding
// it after use.
// It also returns the raw DEK, so the caller can open and cache the db directly in creation scenarios, skipping
// another Argon2id derivation.
func WrapNewDEK(boxID string, kek []byte) (*conf.BoxEncryption, []byte, error) {
	dek, err := util.GenerateDEK()
	if err != nil {
		return nil, nil, err
	}
	wrapped, err := util.EncryptWithAAD(kek, dek, wrappedDEKAAD(boxID))
	if err != nil {
		return nil, nil, err
	}
	return &conf.BoxEncryption{
		Spec:       boxEncryptionSpec,
		WrappedDEK: wrapped,
		WrapNonce:  mustEncryptionNonce(wrapped),
		CreatedAt:  time.Now().UnixMilli(),
	}, dek, nil
}

func wrappedDEKAAD(boxID string) []byte {
	return []byte("siyuan:v1:wrapped-dek:" + boxID)
}

func decryptWrappedDEK(boxID string, enc *conf.BoxEncryption, kek []byte) ([]byte, error) {
	if enc.Spec >= boxEncryptionSpec {
		return util.DecryptWithAAD(kek, enc.WrappedDEK, wrappedDEKAAD(boxID))
	}
	return util.DecryptWithAAD(kek, enc.WrappedDEK, wrappedDEKAAD(boxID))
}

// mustEncryptionNonce extracts the nonce from ciphertext that was just successfully generated. A malformed ciphertext
// here means an internal invariant has been broken, so it terminates execution directly.
func mustEncryptionNonce(ciphertext []byte) []byte {
	nonce, err := util.EncryptionNonce(ciphertext)
	if err != nil {
		panic("extract encryption nonce failed: " + err.Error())
	}
	return nonce
}

// GetDEK retrieves the cached DEK. Returns a copy, so external zeroing doesn't affect the cache.
// Called during filesys/assets/db encryption and decryption.
func GetDEK(boxID string) ([]byte, error) {
	cachedDEKsLock.RLock()
	defer cachedDEKsLock.RUnlock()
	dek, ok := cachedDEKs[boxID]
	if !ok {
		return nil, errors.New("no DEK cached for box " + boxID)
	}
	ret := make([]byte, len(dek))
	copy(ret, dek)
	return ret, nil
}

// ClearDEK clears the DEK for the given notebook. Called when unmounting a single encrypted notebook.
func ClearDEK(boxID string) {
	LockBox(boxID)
}

// ChangeMasterPassword changes the master password: after verifying the old password, it derives a new KEK from the new
// password, re-encrypts the verifier, and rewraps every encrypted notebook's WrappedDEK with the new KEK, writing it
// back to each notebook's BoxConf.
//
// Uses a two-phase commit to ensure recoverability after a crash:
//
//	Phase 0: precompute all new WrappedDEKs (in memory)
//	Phase 1: write the migration manifest
//	Phase 2: switch the global verifier
//	Phase 3: write each box's conf + backup
//	Phase 4: clear the manifest
//
// Note: must be called while all encrypted notebooks are already Unmounted (no DEK in memory), otherwise switching
// between the old and new KEK would make the cache and disk inconsistent.
func ChangeMasterPassword(oldPassword, newPassword string) error {
	if len(newPassword) == 0 {
		return errors.New("new password must not be empty")
	}

	notebookCryptoMu.Lock()
	defer notebookCryptoMu.Unlock()

	// No encrypted notebook may be mounted during a password change (DEK in memory), otherwise switching between the
	// old and new KEK would make the cache and disk inconsistent
	cachedDEKsLock.RLock()
	dekCount := len(cachedDEKs)
	cachedDEKsLock.RUnlock()
	if dekCount > 0 {
		return errors.New("cannot change master password while encrypted notebooks are unlocked (DEKs in memory), lock them first")
	}

	oldKEK, err := deriveKEK(oldPassword)
	if err != nil {
		return err
	}
	defer zeroAndClear(oldKEK)

	Conf.m.Lock()
	nc := Conf.NotebookCrypto
	Conf.m.Unlock()

	params, validErr := util.ValidateArgon2Params(nc.KDFParams)
	if validErr != nil {
		return validErr
	}
	newKEK := util.DeriveKey(newPassword, nc.MasterSalt, params)
	defer zeroAndClear(newKEK)
	newVerifier, err := util.EncryptWithAAD(newKEK, kekVerifierMagic, []byte("siyuan:v1:kek-verifier"))
	if err != nil {
		return err
	}

	// Phase 0: iterate over all encrypted notebooks (including ones with corrupted conf but an existing backup),
	// precomputing the new WrappedDEK (in-memory operation)
	// entries may be empty: the user may have enabled the encryption feature but not yet created an encrypted notebook,
	// in which case the global verifier and backup still need to be updated.
	encBoxIDs, listErr := listAllEncryptedBoxIDs()
	if listErr != nil {
		return fmt.Errorf("list encrypted notebooks failed: %w", listErr)
	}
	var entries []migrationBoxEntry
	for _, id := range encBoxIDs {
		dek, _, dErr := decryptBoxCrypt(id, oldKEK)
		if dErr != nil {
			return errors.New(Conf.Language(316) + " [box=" + id + "]")
		}
		newWrapped, nErr := util.EncryptWithAAD(newKEK, dek, wrappedDEKAAD(id))
		if nErr != nil {
			return nErr
		}
		entries = append(entries, migrationBoxEntry{
			BoxID:         id,
			NewSpec:       boxEncryptionSpec,
			NewWrappedDEK: newWrapped,
			NewWrapNonce:  mustEncryptionNonce(newWrapped),
		})
	}

	// Phase 1: persist the migration manifest (the basis for recovery after a crash)
	newParamsJSON, _ := gulu.JSON.MarshalJSON(params)
	mig := &masterPasswordMigration{
		OldVerifier:      nc.KEKVerifier,
		NewVerifier:      newVerifier,
		NewVerifierNonce: mustEncryptionNonce(newVerifier),
		NewKDFParams:     newParamsJSON,
		Boxes:            entries,
	}
	if err = writeMasterPasswordMigration(mig); err != nil {
		return err
	}

	// Phase 2: switch the global verifier
	Conf.m.Lock()
	Conf.NotebookCrypto.KEKVerifier = newVerifier
	Conf.NotebookCrypto.VerifierNonce = mustEncryptionNonce(newVerifier)
	Conf.NotebookCrypto.KDFParams = params
	Conf.m.Unlock()

	// Conf.Save locks Conf.m internally, so it must not be called while already holding the lock (RWMutex is not
	// reentrant)
	Conf.Save()

	// Phase 3: write each box's conf + backup
	for _, entry := range entries {
		box := &Box{ID: entry.BoxID}
		boxConf := box.GetConf()
		if !boxConf.Encrypted || boxConf.BoxCrypt == nil {
			// conf missing/corrupted: try to rebuild from the per-notebook backup
			backup, bErr := readNotebookCryptBackup(entry.BoxID)
			if bErr == nil && backup != nil && len(backup.WrappedDEK) > 0 {
				boxConf = box.GetConf()
				boxConf.Encrypted = true
				boxConf.BoxCrypt = backup
				if saveErr := box.SaveConf(boxConf); saveErr != nil {
					return fmt.Errorf("%w: %s", errMasterPasswordMigrationPending,
						fmt.Sprintf(Conf.Language(320), entry.BoxID+": rebuild encrypted conf from backup failed: "+saveErr.Error()))
				}
			} else {
				// Neither conf nor backup is available: the manifest is the authoritative source for this box's
				// encryption key, so rebuild BoxCrypt directly from the entry, avoiding an interrupted password change
				// due to transient conf corruption (see the symmetric handling in recoverMasterPasswordMigration).
				logging.LogWarnf("rebuild encrypted box [%s] from migration entry (conf and backup both unavailable)", entry.BoxID)
				boxConf = box.GetConf()
				boxConf.Encrypted = true
				boxConf.BoxCrypt = &conf.BoxEncryption{
					WrappedDEK: entry.NewWrappedDEK,
					WrapNonce:  entry.NewWrapNonce,
					Spec:       entry.NewSpec,
					CreatedAt:  time.Now().UnixMilli(),
				}
				if saveErr := box.SaveConf(boxConf); saveErr != nil {
					return fmt.Errorf("%w: %s", errMasterPasswordMigrationPending,
						fmt.Sprintf(Conf.Language(320), entry.BoxID+": rebuild encrypted conf from migration entry failed: "+saveErr.Error()))
				}
			}
		}
		boxConf.BoxCrypt.WrappedDEK = entry.NewWrappedDEK
		boxConf.BoxCrypt.Spec = entry.NewSpec
		boxConf.BoxCrypt.WrapNonce = entry.NewWrapNonce
		if err = box.SaveConf(boxConf); err != nil {
			return fmt.Errorf("%w: %s", errMasterPasswordMigrationPending,
				fmt.Sprintf(Conf.Language(320), entry.BoxID+": save conf failed: "+err.Error()))
		}
		if err = writeNotebookCryptBackup(entry.BoxID, boxConf.BoxCrypt); err != nil {
			return fmt.Errorf("%w: %s", errMasterPasswordMigrationPending,
				fmt.Sprintf(Conf.Language(320), entry.BoxID+": update notebook crypt backup failed: "+err.Error()))
		}
	}

	// Phase 4: persist the global backup first, then clear the manifest, ensuring recoverability after a crash
	if err = saveNotebookCryptoBackup(newKEK); err != nil {
		return fmt.Errorf("%w: %s", errMasterPasswordMigrationPending,
			fmt.Sprintf(Conf.Language(320), "save notebook crypto backup failed: "+err.Error()))
	}
	removeMasterPasswordMigration()
	return nil
}

// IsEncryptedBox determines whether the given boxID is an encrypted notebook.
// It reads conf.json first, falling back to the standalone backup if missing/corrupted, to avoid failing open.
func IsEncryptedBox(boxID string) bool {
	box := &Box{ID: boxID}
	boxConf := box.GetConf()
	if boxConf != nil && boxConf.Encrypted {
		return true
	}
	// When the primary conf is missing/corrupted, check the standalone backup to confirm whether it's an encrypted
	// notebook
	backupPath := notebookCryptBackupPath(boxID)
	if !filelock.IsExist(backupPath) {
		return false // no backup file -> not encrypted
	}
	backup, err := readNotebookCryptBackup(boxID)
	if err != nil {
		logging.LogWarnf("failed to read notebook crypt backup for [%s]: %s", boxID, err)
		return true // backup exists but is unreadable -> fail-closed
	}
	return backup != nil && len(backup.WrappedDEK) > 0
}

// GetBoxEncryption retrieves the BoxEncryption (including WrappedDEK) of an encrypted notebook.
// It reads conf.json first, falling back to the per-notebook backup if missing/corrupted.
// Returns nil to mean the box is not encrypted; if conf marks it encrypted but the key material is missing, it returns
// an explicit error.
func GetBoxEncryption(boxID string) (*conf.BoxEncryption, error) {
	box := &Box{ID: boxID}
	boxConf := box.GetConf()
	confMarkedEncrypted := boxConf != nil && boxConf.Encrypted

	// conf has a complete BoxCrypt
	if confMarkedEncrypted && boxConf.BoxCrypt != nil && len(boxConf.BoxCrypt.WrappedDEK) > 0 {
		return boxConf.BoxCrypt, nil
	}

	// fall back to the backup
	backup, err := readNotebookCryptBackup(boxID)
	if err != nil {
		return nil, err
	}
	if backup != nil && len(backup.WrappedDEK) > 0 {
		return backup, nil
	}

	// backup is also unavailable
	if confMarkedEncrypted {
		// conf marks it encrypted but the key material is missing -> explicit error (rather than falsely reporting "not encrypted")
		return nil, errors.New("encrypted notebook has no valid key material")
	}
	return nil, nil // a genuinely non-encrypted notebook
}

// needWriteNotebookCryptBackup checks whether the per-notebook backup needs to be written/refreshed.
// Returns true if the backup doesn't exist, or its content differs from crypt.
func needWriteNotebookCryptBackup(boxID string, crypt *conf.BoxEncryption) bool {
	existing, err := readNotebookCryptBackup(boxID)
	if err != nil || existing == nil {
		return true
	}
	return !bytes.Equal(existing.WrappedDEK, crypt.WrappedDEK) ||
		!bytes.Equal(existing.WrapNonce, crypt.WrapNonce) ||
		existing.CreatedAt != crypt.CreatedAt
}

// DeepCopyBoxEncryption deep-copies a BoxEncryption (including its []byte fields), returning nil for a nil input.
// Used by the api layer to save an immutable snapshot of the encryption fields before deserializing the request body.
func DeepCopyBoxEncryption(src *conf.BoxEncryption) *conf.BoxEncryption {
	if src == nil {
		return nil
	}
	return &conf.BoxEncryption{
		Spec:       src.Spec,
		WrappedDEK: append([]byte(nil), src.WrappedDEK...),
		WrapNonce:  append([]byte(nil), src.WrapNonce...),
		CreatedAt:  src.CreatedAt,
	}
}

// listAllEncryptedBoxIDs scans the data directory for every box directory containing notebook-crypt-backup.json,
// filling in any encrypted notebooks with corrupted conf that ListNotebooks may have missed. Used by critical paths
// such as changing/disabling the password and detection.
// Uses IsEncryptedBox uniformly as the single source of truth for "is it encrypted".
func listAllEncryptedBoxIDs() ([]string, error) {
	var ids []string
	seen := map[string]bool{}

	// Pass 1: notebooks already returned by ListNotebooks
	boxes, err := ListNotebooks()
	if err != nil {
		return nil, err
	}
	for _, b := range boxes {
		seen[b.ID] = true
		if IsEncryptedBox(b.ID) {
			ids = append(ids, b.ID)
		}
	}
	// Pass 2: scan backup files to fill in what ListNotebooks missed
	dirs, err := os.ReadDir(util.DataDir)
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		if !dir.IsDir() || !ast.IsNodeIDPattern(dir.Name()) || seen[dir.Name()] {
			continue
		}
		if IsEncryptedBox(dir.Name()) {
			ids = append(ids, dir.Name())
		}
	}
	return ids, nil
}

// ListAllEncryptedBoxIDs returns every enumerable encrypted notebook. On scan failure it logs the error and returns an
// empty list;
// callers involved in key overwriting or deletion must use listAllEncryptedBoxIDs directly and handle the error.
func ListAllEncryptedBoxIDs() []string {
	ids, err := listAllEncryptedBoxIDs()
	if err != nil {
		logging.LogErrorf("list encrypted notebooks failed: %s", err)
		return nil
	}
	return ids
}

// IsSameCryptoBoundary determines whether srcBox and dstBox are within the same encryption boundary (i.e. whether a
// cross-box operation is safe).
// It's allowed between two normal notebooks (neither encrypted); an encrypted notebook only allows operations within
// the same box -- two different encrypted notebooks each have their own independent DEK, and are "outside each other's
// encryption boundary", so moving/merging across boxes would use the wrong DEK and corrupt the ciphertext. Used by
// cross-box operations such as MoveDocs/Doc2Heading for validation.
func IsSameCryptoBoundary(srcBox, dstBox string) bool {
	srcEnc := IsEncryptedBox(srcBox)
	dstEnc := IsEncryptedBox(dstBox)
	if !srcEnc && !dstEnc {
		return true // normal <-> normal: allowed
	}
	return srcEnc && dstEnc && srcBox == dstBox // encrypted: only allowed within the same box
}

// IsBlockRefCrossingBoundary determines whether referencing defBlockID from srcBoxID crosses an encryption boundary.
// Cross-boundary block references are forbidden for encrypted notebooks in both directions: a block in an encrypted
// notebook may only reference blocks within the same encrypted notebook, and a block in a normal box may not reference
// a block in an encrypted notebook.
// Used as a last-resort check when a transaction is persisted, to prevent manual input/drag-and-drop/paste/direct API
// calls from bypassing the frontend's search-based routing.
func IsBlockRefCrossingBoundary(srcBoxID, defBlockID string) bool {
	if "" == defBlockID {
		return false
	}
	if IsEncryptedBox(srcBoxID) {
		// Source is in an encrypted box: the def block must be in the same encrypted box (checked via the encrypted
		// blocktree db)
		bt := treenode.GetBlockTreeInBox(defBlockID, srcBoxID)
		return nil == bt || bt.BoxID != srcBoxID
	}
	// Source is in a normal box: the def block must be in a normal box (checked via the global blocktree, and its box
	// must not be encrypted)
	bt := treenode.GetBlockTree(defBlockID)
	if nil == bt {
		// When not found globally, iterate over encrypted notebooks to look for it, preventing a missed check in the
		// opposite direction (a normal box referencing a block in an encrypted notebook)
		for _, encBoxID := range treenode.GetOpenedEncryptedBoxIDs() {
			if encBT := treenode.GetBlockTreeInBox(defBlockID, encBoxID); nil != encBT {
				bt = encBT
				break
			}
		}
	}
	if nil == bt {
		// It must fail closed when the normal db has no hit and a locked encrypted blocktree cannot be queried,
		// otherwise merely knowing an encrypted block's ID would let a cross-boundary reference be written into the
		// global plaintext database while the encrypted notebook is locked. New blocks within the same transaction
		// tree are allowed separately by the caller.
		return normalBoxBlockRefCrossesBoundary(nil)
	}
	return normalBoxBlockRefCrossesBoundary(bt)
}

func normalBoxBlockRefCrossesBoundary(bt *treenode.BlockTree) bool {
	return bt == nil || IsEncryptedBox(bt.BoxID)
}

// IsEncryptedAssetPath determines whether the given absolute asset path belongs to an encrypted notebook.
// Used by the server layer to decide whether to skip processing a ciphertext file, e.g. for thumbnails.
func IsEncryptedAssetPath(absPath string) bool {
	boxID := ExtractBoxIDFromAssetsPath(absPath)
	return boxID != "" && IsEncryptedBox(boxID)
}

// GetDEKIfUnlocked returns the DEK (a copy) of an unlocked encrypted notebook.
// For a non-encrypted notebook it returns (nil, nil) -- filesys reads/writes it as-is based on that, transparent to
// normal notebooks.
// For an encrypted but unlocked (DEK not in memory) notebook it returns (nil, error) -- filesys's encryption/decryption
// functions refuse to read/write on error, preventing an encrypted notebook from silently being persisted in plaintext
// while locked (defense in depth, see issue #18034).
func GetDEKIfUnlocked(boxID string) ([]byte, error) {
	if !IsEncryptedBox(boxID) {
		return nil, nil
	}
	cachedDEKsLock.RLock()
	defer cachedDEKsLock.RUnlock()
	dek, ok := cachedDEKs[boxID]
	if !ok {
		return nil, errors.New("encrypted notebook is locked, please unlock it first")
	}
	ret := make([]byte, len(dek))
	copy(ret, dek)
	return ret, nil
}

// HoldBoxReadLock acquires the box read lock, preventing LockBox from clearing the cache/temp files while it is held.
// The caller must call ReleaseBoxReadLock once it has finished producing decrypted output.
func HoldBoxReadLock(boxID string) {
	acquireBoxReadLock(boxID)
}

// ReleaseBoxReadLock releases the box read lock acquired by HoldBoxReadLock.
func ReleaseBoxReadLock(boxID string) {
	releaseBoxReadLock(boxID)
}

// extractBoxIDFromPath derives the boxID from an absolute path under the data directory.
// The path looks like <DataDir>/<boxID>/...; it cuts out the segment right after DataDir.
// Returns an empty string if the path is not under DataDir or doesn't match the expected format.
func extractBoxIDFromPath(absPath string) string {
	return ExtractBoxIDFromAssetsPath(absPath)
}

// ExtractBoxIDFromAssetsPath derives the boxID from an absolute path (.sy or assets) under the data directory.
// Used by the server/api layer to determine whether an asset belongs to an encrypted notebook. The path looks like
// <DataDir>/<boxID>/...;
// returns an empty string if it's not under DataDir or the boxID doesn't match a valid ID pattern.
func ExtractBoxIDFromAssetsPath(absPath string) string {
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

// ExtractBoxIDFromHistoryPath derives the boxID from an absolute path under the history directory.
// The path looks like <HistoryDir>/<timestamp>-<op>/<boxID>/...; it cuts out the segment right after the timestamp
// directory.
// Returns an empty string if the path is not under HistoryDir or the boxID doesn't match a valid ID pattern.
func ExtractBoxIDFromHistoryPath(absPath string) string {
	absPath = filepath.ToSlash(absPath)
	historyDir := filepath.ToSlash(util.HistoryDir)
	rel, err := filepath.Rel(historyDir, absPath)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "..") || rel == "." || rel == "" {
		return ""
	}
	parts := strings.SplitN(rel, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	// parts[0] = timestamp-op, parts[1] = boxID
	boxID := parts[1]
	if !ast.IsNodeIDPattern(boxID) {
		return ""
	}
	return boxID
}

// EncryptFile encrypts .sy document bytes with fileKey (a DEK-derived subkey); the AAD binds boxID + the stable file
// base name (excluding the parent directory).
// relativePath is first passed through filesys.SyAAD to extract the stable file base name (<rootID>.sy) and validate
// it, sharing the same AAD construction entry point with filesys.encryptData/decryptData, ensuring encryption and
// decryption stay consistent.
func EncryptFile(boxID, relativePath string, dek, plaintext []byte) ([]byte, error) {
	fileKey := util.DeriveSubKey(dek, "siyuan/file")
	aad, err := filesys.SyAAD(boxID, relativePath)
	if err != nil {
		return nil, err
	}
	return util.EncryptWithAAD(fileKey, plaintext, []byte(aad))
}

// DecryptFile performs the corresponding decryption.
func DecryptFile(boxID, relativePath string, dek, ciphertext []byte) ([]byte, error) {
	fileKey := util.DeriveSubKey(dek, "siyuan/file")
	aad, err := filesys.SyAAD(boxID, relativePath)
	if err != nil {
		return nil, err
	}
	return util.DecryptWithAAD(fileKey, ciphertext, []byte(aad))
}

// EncryptAsset encrypts asset bytes with assetKey (a DEK-derived subkey); the AAD binds boxID + the on-disk file name.
// diskName is the sanitized file name on disk (for an encrypted box) or the original file name (for a normal box).
func EncryptAsset(boxID, diskName string, dek, plaintext []byte) ([]byte, error) {
	assetKey := util.DeriveSubKey(dek, "siyuan/asset")
	aad := "siyuan:v1:asset:" + boxID + ":assets/" + diskName
	return util.EncryptWithAAD(assetKey, plaintext, []byte(aad))
}

// DecryptAsset performs the corresponding decryption.
func DecryptAsset(boxID, diskName string, dek, ciphertext []byte) ([]byte, error) {
	assetKey := util.DeriveSubKey(dek, "siyuan/asset")
	aad := "siyuan:v1:asset:" + boxID + ":assets/" + diskName
	return util.DecryptWithAAD(assetKey, ciphertext, []byte(aad))
}

func EncryptAssetNameMapping(boxID string, dek, plaintext []byte) ([]byte, error) {
	assetKey := util.DeriveSubKey(dek, "siyuan/asset")
	aad := "siyuan:v1:asset-names:" + boxID
	return util.EncryptWithAAD(assetKey, plaintext, []byte(aad))
}

func DecryptAssetNameMapping(boxID string, dek, ciphertext []byte) ([]byte, error) {
	assetKey := util.DeriveSubKey(dek, "siyuan/asset")
	aad := "siyuan:v1:asset-names:" + boxID
	return util.DecryptWithAAD(assetKey, ciphertext, []byte(aad))
}

// notebookCryptBackupPath returns the standalone BoxCrypt backup path for an encrypted notebook.
// This file serves as both the marker for "this notebook is encrypted" and a fallback recovery source when the
// primary conf.json is lost.
// It works together with the global NotebookCrypto backup (<DataDir>/.siyuan/notebook-crypto-backup.json): the global
// backup stores MasterSalt/KEKVerifier, while the per-notebook backup stores WrappedDEK/WrapNonce.
func notebookCryptBackupPath(boxID string) string {
	return filepath.Join(util.DataDir, boxID, ".siyuan", "notebook-crypt-backup.json")
}

// writeNotebookCryptBackup writes the BoxCrypt backup for an encrypted notebook.
// Only called on notebooks with Encrypted=true, alongside writes from CreateEncryptedBox / ChangeMasterPassword.
func writeNotebookCryptBackup(boxID string, crypt *conf.BoxEncryption) error {
	backupPath := notebookCryptBackupPath(boxID)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		return fmt.Errorf("mkdir notebook crypt backup dir failed: %w", err)
	}
	data, err := gulu.JSON.MarshalIndentJSON(crypt, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal notebook crypt backup failed: %w", err)
	}
	if err := filelock.WriteFile(backupPath, data); err != nil {
		return fmt.Errorf("write notebook crypt backup failed: %w", err)
	}
	return nil
}

// readNotebookCryptBackup reads the BoxCrypt backup for an encrypted notebook.
// Returns (nil, nil) if the backup file doesn't exist; the caller uses this to distinguish "not an encrypted notebook"
// from "backup doesn't exist".
func readNotebookCryptBackup(boxID string) (*conf.BoxEncryption, error) {
	backupPath := notebookCryptBackupPath(boxID)
	if !filelock.IsExist(backupPath) {
		return nil, nil
	}
	data, err := filelock.ReadFile(backupPath)
	if err != nil {
		return nil, fmt.Errorf("read notebook crypt backup failed: %w", err)
	}
	var crypt conf.BoxEncryption
	if err = gulu.JSON.UnmarshalJSON(data, &crypt); err != nil {
		return nil, fmt.Errorf("unmarshal notebook crypt backup failed: %w", err)
	}
	return &crypt, nil
}

// copyAssetDecryptIfEncrypted copies the asset at srcPath to destPath.
// If srcPath is under an unlocked encrypted notebook, it reads the ciphertext -> decrypts it -> writes the plaintext
// to destPath (the export directory);
// otherwise it takes the original filelock.Copy path (byte-level copy, works for either ciphertext or plaintext).
func copyAssetDecryptIfEncrypted(srcPath, destPath string) error {
	boxID := ExtractBoxIDFromAssetsPath(srcPath)
	if boxID != "" && IsEncryptedBox(boxID) {
		HoldBoxReadLock(boxID)
		defer ReleaseBoxReadLock(boxID)
		dek, err := GetDEKIfUnlocked(boxID)
		if err != nil {
			// The encrypted notebook is not unlocked: fail closed and refuse to copy (don't copy the ciphertext, to
			// avoid leaking an unusable file)
			return errors.New(Conf.Language(314))
		}
		raw, readErr := filelock.ReadFile(srcPath)
		if readErr != nil {
			return readErr
		}
		diskName := filepath.Base(srcPath)
		plain, decErr := DecryptAsset(boxID, diskName, dek, raw)
		if decErr != nil {
			return errors.New(Conf.Language(316))
		}
		if err := filelock.WriteFile(destPath, plain); err != nil {
			return err
		}
		return nil
	}
	return filelock.Copy(srcPath, destPath)
}

// CreateEncryptedBox creates a new encrypted notebook. Can be called multiple times to create several.
// Precondition: the encryption feature is enabled. Creation requires the master password (used to temporarily derive
// the KEK for wrapping the DEK, then discarded).
// After creation, the generated DEK is used directly to open and cache the encrypted db (already unlocked); the
// caller can then mount it by calling openNotebook.
func CreateEncryptedBox(name, password string) (id string, err error) {
	notebookCryptoMu.Lock()
	defer notebookCryptoMu.Unlock()

	Conf.m.RLock()
	enabled := Conf.NotebookCrypto.Enabled
	Conf.m.RUnlock()
	if !enabled {
		return "", errors.New(Conf.Language(310))
	}

	kek, err := deriveKEK(password)
	if err != nil {
		return "", err
	}
	defer zeroAndClear(kek)

	id, err = CreateBox(name)
	if err != nil {
		return "", err
	}

	// If a later step fails, clean up the already-created box directory and encrypted db files, avoiding a
	// half-created state
	boxCreated := true
	defer func() {
		if err != nil && boxCreated {
			sql.RemoveEncryptedDBFile(id)
			treenode.RemoveEncryptedBlockTreeDBFile(id)
			boxDir := filepath.Join(util.DataDir, id)
			if rmErr := filelock.Remove(boxDir); rmErr != nil {
				logging.LogErrorf("cleanup failed encrypted box [%s]: %s", id, rmErr)
			}
			id = ""
		}
	}()

	enc, dek, err := WrapNewDEK(id, kek)
	if err != nil {
		return "", err
	}

	box := &Box{ID: id}
	boxConf := box.GetConf()
	boxConf.Encrypted = true
	boxConf.BoxCrypt = enc
	if err = box.SaveConf(boxConf); err != nil {
		return "", fmt.Errorf("save encrypted notebook conf failed: %w", err)
	}
	if err = writeNotebookCryptBackup(id, enc); err != nil {
		return "", fmt.Errorf("write notebook crypt backup failed: %w", err)
	}
	// Read back and verify the encryption config was persisted, to avoid treating it as a normal notebook after a
	// failed write
	verifyConf := box.GetConf()
	if verifyConf == nil || !verifyConf.Encrypted || verifyConf.BoxCrypt == nil {
		err = errors.New("encrypted notebook metadata verification failed after write")
		return "", err
	}

	// Reuse the just-derived DEK to open and cache the db directly, skipping another Argon2id unlock
	cachedDEKsLock.Lock()
	defer cachedDEKsLock.Unlock()
	if err = sql.OpenEncryptedDB(id, dek); err != nil {
		return "", err
	}
	if err = treenode.OpenEncryptedBlockTreeDB(id, dek); err != nil {
		sql.CloseEncryptedDB(id)
		return "", err
	}
	cachedDEKs[id] = dek

	// Initialize the auto-lock access timestamp, symmetric with UnlockBox
	newVal := &atomic.Int64{}
	newVal.Store(time.Now().UnixNano())
	boxLastAccess.Store(id, newVal)

	IncSync()
	return id, nil
}

// zeroAndClear zeroes out the key bytes before clearing the slice, to minimize how long the key remains resident in
// memory.
func zeroAndClear(key []byte) {
	for i := range key {
		key[i] = 0
	}
}

// TouchUnlockedEncryptedBoxes is called by real user interaction or an explicit keep-alive from a headless client, to
// refresh the idle timer of currently unlocked notebooks.
func TouchUnlockedEncryptedBoxes() {
	now := time.Now().UnixNano()
	cachedDEKsLock.RLock()
	boxIDs := make([]string, 0, len(cachedDEKs))
	for boxID := range cachedDEKs {
		boxIDs = append(boxIDs, boxID)
	}
	cachedDEKsLock.RUnlock()
	for _, boxID := range boxIDs {
		if val, ok := boxLastAccess.Load(boxID); ok {
			val.(*atomic.Int64).Store(now)
		}
	}
}

// AutoLockIdleEncryptedBoxesJob checks every unlocked encrypted notebook and automatically locks any that have been
// idle past the timeout.
// Called by cron once a minute. The threshold is controlled by NotebookCrypto.AutoLockMinutes (0 = disabled).
func AutoLockIdleEncryptedBoxesJob() {
	Conf.m.RLock()
	threshold := Conf.NotebookCrypto.AutoLockMinutes
	Conf.m.RUnlock()
	if threshold <= 0 {
		return
	}

	now := time.Now().UnixNano()
	thresholdNs := int64(time.Duration(threshold) * time.Minute)

	cachedDEKsLock.RLock()
	boxIDs := make([]string, 0, len(cachedDEKs))
	for id := range cachedDEKs {
		boxIDs = append(boxIDs, id)
	}
	cachedDEKsLock.RUnlock()

	for _, boxID := range boxIDs {
		if val, ok := boxLastAccess.Load(boxID); ok {
			lastAccess := val.(*atomic.Int64).Load()
			elapsed := now - lastAccess
			if elapsed >= thresholdNs {
				logging.LogInfof("auto-locking idle encrypted notebook [%s] (elapsed=%ds, threshold=%dm)", boxID, elapsed/1e9, threshold)
				// Get the notebook name before Unmount: Unmount closes the notebook, after which Conf.Box returns nil,
				// which would make the message show the boxID instead
				boxName := boxID
				if box := Conf.Box(boxID); nil != box {
					boxName = box.Name
				}
				Unmount(boxID)
				// Auto-lock closes any document currently being edited, so push a notification to prevent the user from
				// thinking the app crashed
				util.PushMsg(fmt.Sprintf(Conf.Language(322), boxName), 0)
			}
		}
	}
}

// SetAutoLockMinutes sets the idle-minutes threshold for auto-locking encrypted notebooks. 0 means disabled.
func SetAutoLockMinutes(minutes int) {
	if minutes < 0 {
		minutes = 0
	}
	Conf.m.Lock()
	Conf.NotebookCrypto.AutoLockMinutes = minutes
	Conf.m.Unlock()
}
