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

package conf

import "github.com/siyuan-note/siyuan/kernel/util"

// NotebookCrypto maintains global key management parameters for encrypted notebooks, persisted with conf.json.
// MasterSalt and KEKVerifier are designed to be stored as plaintext: the salt isn't secret, and the verifier is
// itself ciphertext (a fixed magic number encrypted with the KEK).
type NotebookCrypto struct {
	Enabled         bool              `json:"enabled"`         // Whether the encrypted notebook feature is enabled
	MasterSalt      []byte            `json:"masterSalt"`      // Salt derived from the master password via Argon2id, globally unique
	KDFParams       util.Argon2Params `json:"kdfParams"`       // Argon2id parameters, persisted so derivation is consistent across platforms
	KEKVerifier     []byte            `json:"kekVerifier"`     // A fixed magic number encrypted with the KEK via AES-GCM, used to verify the master password offline
	VerifierNonce   []byte            `json:"verifierNonce"`   // The verifier's GCM nonce (extracted from the encryption envelope)
	AutoLockMinutes int               `json:"autoLockMinutes"` // Idle minutes before an encrypted notebook auto-locks, 0 disables it, default 5

	// Backup integrity fields (Spec>=1)
	// Spec is the backup spec version. Checksum guards against corruption. KEKMAC requires master password verification.
	Spec      int    `json:"spec,omitempty"`      // Backup spec version (see CurrentNotebookCryptoSpec)
	BackupID  string `json:"backupID,omitempty"`  // Unique backup identifier (UUID)
	CreatedAt int64  `json:"createdAt,omitempty"` // Backup creation/update time (unix seconds)
	Checksum  string `json:"checksum,omitempty"`  // SHA-256 checksum
	KEKMAC    []byte `json:"kekMAC,omitempty"`    // KEK HMAC-SHA256 (requires master password verification)
}

// NewNotebookCrypto creates a NotebookCrypto with default Argon2id parameters.
func NewNotebookCrypto() *NotebookCrypto {
	return &NotebookCrypto{
		KDFParams:       util.DefaultArgon2Params(),
		AutoLockMinutes: 5,
		Spec:            CurrentNotebookCryptoSpec,
	}
}

// CurrentNotebookCryptoSpec is the current backup spec version number.
const CurrentNotebookCryptoSpec = 1

// UpgradeSpec centrally handles version-by-version upgrades of the NotebookCrypto backup spec.
// There is currently no supported legacy version to migrate from; when a new spec is added later, field
// conversion should be done here in version order, advancing Spec only after the previous version's data has been
// successfully validated at each step, with the caller persisting the upgrade result.
func UpgradeSpec(nc *NotebookCrypto) (upgraded bool) {
	// Future upgrade pattern:
	// if nc.Spec == 1 {
	// 	// Validate and migrate v1 -> v2.
	// 	nc.Spec = 2
	// 	upgraded = true
	// }
	return
}
