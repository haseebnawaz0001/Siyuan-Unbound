package av

import (
	"maps"
	"sync"

	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/siyuan-note/logging"
)

var (
	AttributeViewBlocksLock = sync.Mutex{}
)

// isSameCryptoBoundary determines whether the box holding the AV definition and the box holding the source
// block are within the same encryption boundary.
// Normal <-> normal is allowed; when encryption is involved, they must be the same box. This is consistent
// with the cross-boundary validation logic in relation.go.
func isSameCryptoBoundary(avBoxID, blockBoxID string) bool {
	if AVIsEncryptedBox == nil {
		return true // Hook not injected (not running normally), allow it to avoid blocking
	}
	avEnc := avBoxID != "" && AVIsEncryptedBox(avBoxID)
	blockEnc := blockBoxID != "" && AVIsEncryptedBox(blockBoxID)
	if !avEnc && !blockEnc {
		return true // Normal <-> normal: allowed
	}
	return avEnc && blockEnc && avBoxID == blockBoxID // Encrypted: only allowed within the same box
}

func GetBlockRels() (ret map[string][]string) {
	AttributeViewBlocksLock.Lock()
	defer AttributeViewBlocksLock.Unlock()

	ret = map[string][]string{}
	// Global mirror index (normal box)
	maps.Copy(ret, readMirrorBlocks(""))
	// Mirror indexes of encrypted notebooks (the ones already opened)
	if AVEncryptedBoxIDs != nil {
		for _, encBoxID := range AVEncryptedBoxIDs() {
			maps.Copy(ret, readMirrorBlocks(encBoxID))
		}
	}
	return
}

func IsMirror(avID string) bool {
	AttributeViewBlocksLock.Lock()
	defer AttributeViewBlocksLock.Unlock()

	_, boxID := FindAttributeViewPath(avID)
	avBlocks := readMirrorBlocks(boxID)
	blockIDs := avBlocks[avID]
	return nil != blockIDs && 1 < len(blockIDs)
}

func RemoveBlockRel(avID, blockID string, existBlockTree func(string) bool) (ret bool) {
	AttributeViewBlocksLock.Lock()
	defer AttributeViewBlocksLock.Unlock()

	_, boxID := FindAttributeViewPath(avID)
	avBlocks := readMirrorBlocks(boxID)

	blockIDs := avBlocks[avID]
	if nil == blockIDs {
		return
	}

	var newBlockIDs []string
	for _, v := range blockIDs {
		if v != blockID {
			if existBlockTree(v) {
				newBlockIDs = append(newBlockIDs, v)
			}
		}
	}
	avBlocks[avID] = newBlockIDs
	ret = len(newBlockIDs) != len(blockIDs)

	if err := writeMirrorBlocks(boxID, avBlocks); err != nil {
		logging.LogErrorf("write attribute view blocks failed: %s", err)
		return
	}
	return
}

func BatchUpsertBlockRel(nodes []*ast.Node) {
	AttributeViewBlocksLock.Lock()
	defer AttributeViewBlocksLock.Unlock()

	// Bucket by boxID: an avID in a normal box writes to the global mirror, an avID in an encrypted notebook
	// writes to the notebook-level mirror
	boxAvBlocks := map[string]map[string][]string{} // boxID -> avBlocks

	for _, n := range nodes {
		if ast.NodeAttributeView != n.Type {
			continue
		}

		if "" == n.AttributeViewID || "" == n.ID {
			continue
		}

		_, avBoxID := FindAttributeViewPath(n.AttributeViewID)
		// Cross-encryption-boundary check: the box of the source block (the AV block node itself) must be
		// within the same encryption boundary as the AV definition, otherwise an encrypted block ID could
		// leak into the global plaintext mirror index (or vice versa)
		if AVGetBlockBoxID != nil {
			blockBoxID := AVGetBlockBoxID(n.ID)
			if !isSameCryptoBoundary(avBoxID, blockBoxID) {
				logging.LogWarnf("skip cross-boundary AV mirror: avID=%s(avBox=%s) block=%s(blockBox=%s)",
					n.AttributeViewID, avBoxID, n.ID, blockBoxID)
				continue
			}
		}
		boxID := avBoxID
		avBlocks, ok := boxAvBlocks[boxID]
		if !ok {
			avBlocks = readMirrorBlocks(boxID)
			boxAvBlocks[boxID] = avBlocks
		}

		blockIDs := avBlocks[n.AttributeViewID]
		blockIDs = append(blockIDs, n.ID)
		blockIDs = gulu.Str.RemoveDuplicatedElem(blockIDs)
		avBlocks[n.AttributeViewID] = blockIDs
	}

	for boxID, avBlocks := range boxAvBlocks {
		if err := writeMirrorBlocks(boxID, avBlocks); err != nil {
			logging.LogErrorf("write attribute view blocks failed: %s", err)
		}
	}
}

func UpsertBlockRel(avID, blockID string) (ret bool) {
	AttributeViewBlocksLock.Lock()
	defer AttributeViewBlocksLock.Unlock()

	_, avBoxID := FindAttributeViewPath(avID)
	// Cross-encryption-boundary check: the source block's box must be within the same encryption boundary as
	// the AV definition
	if AVGetBlockBoxID != nil {
		blockBoxID := AVGetBlockBoxID(blockID)
		if !isSameCryptoBoundary(avBoxID, blockBoxID) {
			logging.LogWarnf("skip cross-boundary AV mirror: avID=%s(avBox=%s) block=%s(blockBox=%s)",
				avID, avBoxID, blockID, blockBoxID)
			return
		}
	}
	boxID := avBoxID
	avBlocks := readMirrorBlocks(boxID)

	blockIDs := avBlocks[avID]
	oldLen := len(blockIDs)
	blockIDs = append(blockIDs, blockID)
	blockIDs = gulu.Str.RemoveDuplicatedElem(blockIDs)
	avBlocks[avID] = blockIDs
	ret = oldLen != len(blockIDs) && 0 != oldLen

	if err := writeMirrorBlocks(boxID, avBlocks); err != nil {
		logging.LogErrorf("write attribute view blocks failed: %s", err)
		return
	}
	return
}
