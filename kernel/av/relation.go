package av

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/88250/gulu"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/util"
	"github.com/vmihailenco/msgpack/v5"
)

var (
	attributeViewRelationsLock = sync.Mutex{}
)

// relationsPath returns the path to the relations index file, bucketed by the box that owns the AV.
// Encrypted box: <DataDir>/<boxID>/storage/av/relations.msgpack (DEK-encrypted)
// Normal box: <DataDir>/storage/av/relations.msgpack (plaintext)
func relationsPath(boxID string) string {
	if boxID != "" {
		return filepath.Join(util.DataDir, boxID, "storage", "av", "relations.msgpack")
	}
	return filepath.Join(util.DataDir, "storage", "av", "relations.msgpack")
}

// readRelations reads the relations index (automatically decrypted when boxID is non-empty).
func readRelations(boxID string) (avRels map[string][]string) {
	avRels = map[string][]string{}
	p := relationsPath(boxID)
	if !filelock.IsExist(p) {
		return
	}
	data, err := filelock.ReadFile(p)
	if err != nil {
		logging.LogErrorf("read attribute view relations failed: %s", err)
		return
	}
	if boxID != "" {
		dec, decErr := decryptAVData(boxID, "relation", data)
		if decErr != nil {
			logging.LogErrorf("decrypt attribute view relations failed: %s", decErr)
			return
		}
		data = dec
	}
	if err = msgpack.Unmarshal(data, &avRels); err != nil {
		logging.LogErrorf("unmarshal attribute view relations failed: %s", err)
		return
	}
	return
}

// writeRelations writes the relations index (encrypted when boxID is non-empty).
func writeRelations(boxID string, avRels map[string][]string) {
	p := relationsPath(boxID)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		logging.LogErrorf("create attribute view dir failed: %s", err)
		return
	}
	data, err := msgpack.Marshal(avRels)
	if err != nil {
		logging.LogErrorf("marshal attribute view relations failed: %s", err)
		return
	}
	if boxID != "" {
		enc, encErr := encryptAVData(boxID, "relation", data)
		if encErr != nil {
			logging.LogErrorf("encrypt attribute view relations failed: %s", encErr)
			return
		}
		data = enc
	}
	if err = filelock.WriteFile(p, data); err != nil {
		logging.LogErrorf("write attribute view relations failed: %s", err)
		return
	}
}

// relationsBoxIDByAvID looks up the owning boxID from destAvID, determining where the relations index is
// stored.
// For an AV in an encrypted notebook it returns that boxID; for an AV in a normal box it returns an empty
// string (the global path).
func relationsBoxIDByAvID(avID string) string {
	_, boxID := FindAttributeViewPath(avID)
	return boxID
}

func GetSrcAvIDs(destAvID string) []string {
	attributeViewRelationsLock.Lock()
	defer attributeViewRelationsLock.Unlock()

	boxID := relationsBoxIDByAvID(destAvID)
	avRels := readRelations(boxID)
	srcAvIDs := avRels[destAvID]
	if nil == srcAvIDs {
		return nil
	}
	return srcAvIDs
}

func RemoveAvRel(srcAvID, destAvID string) {
	attributeViewRelationsLock.Lock()
	defer attributeViewRelationsLock.Unlock()

	boxID := relationsBoxIDByAvID(destAvID)
	avRels := readRelations(boxID)

	srcAvIDs := avRels[destAvID]
	if nil == srcAvIDs {
		return
	}

	var newAvIDs []string
	for _, v := range srcAvIDs {
		if v != srcAvID {
			newAvIDs = append(newAvIDs, v)
		}
	}
	avRels[destAvID] = newAvIDs
	writeRelations(boxID, avRels)
}

func UpsertAvBackRel(srcAvID, destAvID string) {
	attributeViewRelationsLock.Lock()
	defer attributeViewRelationsLock.Unlock()

	// Reject cross-encryption-boundary relations: src and dest must be within the same encryption boundary
	// (both normal, or the same encrypted box), otherwise the relation could leak the existence/structure of
	// an encrypted AV into a normal library or another encrypted box
	_, srcBox := FindAttributeViewPath(srcAvID)
	_, destBox := FindAttributeViewPath(destAvID)
	if AVIsEncryptedBox != nil {
		srcEnc := srcBox != "" && AVIsEncryptedBox(srcBox)
		destEnc := destBox != "" && AVIsEncryptedBox(destBox)
		if srcEnc != destEnc || (srcEnc && destEnc && srcBox != destBox) {
			logging.LogWarnf("skip cross-boundary AV relation: src=%s(box=%s) dest=%s(box=%s)", srcAvID, srcBox, destAvID, destBox)
			return
		}
	}

	boxID := destBox
	avRels := readRelations(boxID)

	srcAvIDs := avRels[destAvID]
	srcAvIDs = append(srcAvIDs, srcAvID)
	srcAvIDs = gulu.Str.RemoveDuplicatedElem(srcAvIDs)
	avRels[destAvID] = srcAvIDs
	writeRelations(boxID, avRels)
}
