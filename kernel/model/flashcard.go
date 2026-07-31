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
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/88250/lute/parse"
	"github.com/open-spaced-repetition/go-fsrs/v3"
	"github.com/siyuan-note/filelock"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/riff"
	"github.com/siyuan-note/siyuan/kernel/cache"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/treenode"
	"github.com/siyuan-note/siyuan/kernel/util"
)

func GetFlashcardsByBlockIDs(blockIDs []string) (ret []*Block) {
	deckLock.Lock()
	defer deckLock.Unlock()

	waitForSyncingStorages()

	ret = []*Block{}
	deck := Decks[builtinDeckID]
	if nil == deck {
		return
	}

	cards := deck.GetCardsByBlockIDs(blockIDs)
	blocks, _, _ := getCardsBlocks(cards, 1, math.MaxInt)

	for _, blockID := range blockIDs {
		found := false
		for _, block := range blocks {
			if blockID == block.ID {
				found = true
				ret = append(ret, block)
				break
			}
		}
		if !found {
			ret = append(ret, &Block{
				ID:      blockID,
				Content: Conf.Language(180),
			})
		}
	}
	return
}

type SetFlashcardDueTime struct {
	ID  string `json:"id"`  // Card ID
	Due string `json:"due"` // Next review time, formatted as YYYYMMDDHHmmss
}

func SetFlashcardsDueTime(cardDues []*SetFlashcardDueTime) (err error) {
	// Add internal kernel API `/api/riff/batchSetRiffCardsDueTime` https://github.com/siyuan-note/siyuan/issues/10423

	deckLock.Lock()
	defer deckLock.Unlock()

	waitForSyncingStorages()

	deck := Decks[builtinDeckID]
	if nil == deck {
		return
	}

	for _, cardDue := range cardDues {
		card := deck.GetCard(cardDue.ID)
		if nil == card {
			continue
		}

		due, parseErr := time.ParseInLocation("20060102150405", cardDue.Due, time.Local)
		if nil != parseErr {
			logging.LogErrorf("parse due time [%s] failed: %s", cardDue.Due, err)
			err = parseErr
			return
		}

		card.SetDue(due)
	}

	if err = deck.Save(); err != nil {
		logging.LogErrorf("save deck [%s] failed: %s", builtinDeckID, err)
	}
	return
}

func ResetFlashcards(typ, id, deckID string, blockIDs []string) {
	// Support resetting the learning progress of flashcards https://github.com/siyuan-note/siyuan/issues/9564

	if 0 < len(blockIDs) {
		if "" == deckID {
			// No deck ID is given when entering from global management, so we need to iterate over all decks
			for _, deck := range Decks {
				allBlockIDs := deck.GetBlockIDs()
				for _, blockID := range blockIDs {
					if gulu.Str.Contains(blockID, allBlockIDs) {
						deckID = deck.ID
						break
					}
				}
				if "" == deckID {
					logging.LogWarnf("deck not found for blocks [%s]", strings.Join(blockIDs, ","))
					continue
				}
				resetFlashcards(deckID, blockIDs)
			}
			return
		}

		resetFlashcards(deckID, blockIDs)
		return
	}

	var blocks []*Block
	switch typ {
	case "notebook":
		for i := 1; ; i++ {
			pagedBlocks, _, _ := GetNotebookFlashcards(id, i, 20)
			if 1 > len(pagedBlocks) {
				break
			}
			blocks = append(blocks, pagedBlocks...)
		}
		for _, block := range blocks {
			blockIDs = append(blockIDs, block.ID)
		}
	case "tree":
		for i := 1; ; i++ {
			pagedBlocks, _, _ := GetTreeFlashcards(id, i, 20)
			if 1 > len(pagedBlocks) {
				break
			}
			blocks = append(blocks, pagedBlocks...)
		}
		for _, block := range blocks {
			blockIDs = append(blockIDs, block.ID)
		}
	case "deck":
		for i := 1; ; i++ {
			pagedBlocks, _, _ := GetDeckFlashcards(id, i, 20)
			if 1 > len(pagedBlocks) {
				break
			}
			blocks = append(blocks, pagedBlocks...)
		}
	default:
		logging.LogErrorf("invalid type [%s]", typ)
	}

	blockIDs = gulu.Str.RemoveDuplicatedElem(blockIDs)
	resetFlashcards(deckID, blockIDs)
}

func resetFlashcards(deckID string, blockIDs []string) {
	transactions := []*Transaction{
		{
			DoOperations: []*Operation{
				{
					Action:   "removeFlashcards",
					DeckID:   deckID,
					BlockIDs: blockIDs,
				},
			},
		},
		{
			DoOperations: []*Operation{
				{
					Action:   "addFlashcards",
					DeckID:   deckID,
					BlockIDs: blockIDs,
				},
			},
		},
	}

	PerformTransactions(&transactions)
	FlushTxQueue()
}

func GetFlashcardNotebooks() (ret []*Box) {
	deck := Decks[builtinDeckID]
	if nil == deck {
		return
	}

	deckBlockIDs := deck.GetBlockIDs()
	boxes := Conf.GetOpenedBoxes()
	for _, box := range boxes {
		// Encrypted notebooks don't support flashcards, so don't show them in the flashcard notebook list
		if IsEncryptedBox(box.ID) {
			continue
		}
		newFlashcardCount, dueFlashcardCount, flashcardCount := countBoxFlashcard(box.ID, deck, deckBlockIDs)
		if 0 < flashcardCount {
			box.NewFlashcardCount = newFlashcardCount
			box.DueFlashcardCount = dueFlashcardCount
			box.FlashcardCount = flashcardCount
			ret = append(ret, box)
		}
	}
	return
}

func countTreeFlashcard(rootID string, deck *riff.Deck, deckBlockIDs []string) (newFlashcardCount, dueFlashcardCount, flashcardCount int) {
	blockIDsMap, blockIDs := getTreeSubTreeChildBlocks(rootID)
	for _, deckBlockID := range deckBlockIDs {
		if blockIDsMap[deckBlockID] {
			flashcardCount++
		}
	}
	if 1 > flashcardCount {
		return
	}

	newFlashCards := deck.GetNewCardsByBlockIDs(blockIDs)
	newFlashcardCount = len(newFlashCards)
	newDueFlashcards := deck.GetDueCardsByBlockIDs(blockIDs)
	dueFlashcardCount = len(newDueFlashcards)
	return
}

func countBoxFlashcard(boxID string, deck *riff.Deck, deckBlockIDs []string) (newFlashcardCount, dueFlashcardCount, flashcardCount int) {
	blockIDsMap, blockIDs := getBoxBlocks(boxID)
	for _, deckBlockID := range deckBlockIDs {
		if blockIDsMap[deckBlockID] {
			flashcardCount++
		}
	}
	if 1 > flashcardCount {
		return
	}

	newFlashCards := deck.GetNewCardsByBlockIDs(blockIDs)
	newFlashcardCount = len(newFlashCards)
	newDueFlashcards := deck.GetDueCardsByBlockIDs(blockIDs)
	dueFlashcardCount = len(newDueFlashcards)
	return
}

var (
	Decks    = map[string]*riff.Deck{}
	deckLock = sync.Mutex{}
)

func GetNotebookFlashcards(boxID string, page, pageSize int) (blocks []*Block, total, pageCount int) {
	blocks = []*Block{}

	entries, err := os.ReadDir(filepath.Join(util.DataDir, boxID))
	if err != nil {
		logging.LogErrorf("read dir failed: %s", err)
		return
	}

	var rootIDs []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".sy") {
			continue
		}

		rootIDs = append(rootIDs, strings.TrimSuffix(entry.Name(), ".sy"))
	}

	var treeBlockIDs []string
	for _, rootID := range rootIDs {
		_, blockIDs := getTreeSubTreeChildBlocks(rootID)
		treeBlockIDs = append(treeBlockIDs, blockIDs...)
	}
	treeBlockIDs = gulu.Str.RemoveDuplicatedElem(treeBlockIDs)

	deck := Decks[builtinDeckID]
	if nil == deck {
		return
	}

	var allBlockIDs []string
	deckBlockIDs := deck.GetBlockIDs()
	for _, blockID := range deckBlockIDs {
		if gulu.Str.Contains(blockID, treeBlockIDs) {
			allBlockIDs = append(allBlockIDs, blockID)
		}
	}
	allBlockIDs = gulu.Str.RemoveDuplicatedElem(allBlockIDs)
	cards := deck.GetCardsByBlockIDs(allBlockIDs)

	blocks, total, pageCount = getCardsBlocks(cards, page, pageSize)
	return
}

func GetTreeFlashcards(rootID string, page, pageSize int) (blocks []*Block, total, pageCount int) {
	blocks = []*Block{}
	cards := getTreeSubTreeFlashcards(rootID)
	blocks, total, pageCount = getCardsBlocks(cards, page, pageSize)
	return
}

func getTreeSubTreeFlashcards(rootID string) (ret []riff.Card) {
	deck := Decks[builtinDeckID]
	if nil == deck {
		return
	}

	var allBlockIDs []string
	deckBlockIDs := deck.GetBlockIDs()
	treeBlockIDsMap, _ := getTreeSubTreeChildBlocks(rootID)
	for _, blockID := range deckBlockIDs {
		if treeBlockIDsMap[blockID] {
			allBlockIDs = append(allBlockIDs, blockID)
		}
	}
	allBlockIDs = gulu.Str.RemoveDuplicatedElem(allBlockIDs)
	ret = deck.GetCardsByBlockIDs(allBlockIDs)
	return
}

func getTreeFlashcards(rootID string) (ret []riff.Card) {
	deck := Decks[builtinDeckID]
	if nil == deck {
		return
	}

	var allBlockIDs []string
	deckBlockIDs := deck.GetBlockIDs()
	treeBlockIDsMap, _ := getTreeBlocks(rootID)
	for _, blockID := range deckBlockIDs {
		if treeBlockIDsMap[blockID] {
			allBlockIDs = append(allBlockIDs, blockID)
		}
	}
	allBlockIDs = gulu.Str.RemoveDuplicatedElem(allBlockIDs)
	ret = deck.GetCardsByBlockIDs(allBlockIDs)
	return
}

func GetDeckFlashcards(deckID string, page, pageSize int) (blocks []*Block, total, pageCount int) {
	blocks = []*Block{}
	var cards []riff.Card
	if "" == deckID {
		for _, deck := range Decks {
			blockIDs := deck.GetBlockIDs()
			cards = append(cards, deck.GetCardsByBlockIDs(blockIDs)...)
		}
	} else {
		deck := Decks[deckID]
		if nil == deck {
			return
		}

		blockIDs := deck.GetBlockIDs()
		cards = append(cards, deck.GetCardsByBlockIDs(blockIDs)...)
	}

	blocks, total, pageCount = getCardsBlocks(cards, page, pageSize)
	return
}

func getCardsBlocks(cards []riff.Card, page, pageSize int) (blocks []*Block, total, pageCount int) {
	// sort by due date asc https://github.com/siyuan-note/siyuan/pull/9673
	sort.Slice(cards, func(i, j int) bool {
		due1 := cards[i].(*riff.FSRSCard).C.Due
		due2 := cards[j].(*riff.FSRSCard).C.Due
		if due1.IsZero() || due2.IsZero() {
			// Improve flashcard management sorting https://github.com/siyuan-note/siyuan/issues/14686
			cid1 := cards[i].ID()
			cid2 := cards[j].ID()
			return cid1 < cid2
		}
		return due1.Before(due2)
	})

	total = len(cards)
	pageCount = int(math.Ceil(float64(total) / float64(pageSize)))
	start := (page - 1) * pageSize
	end := page * pageSize
	if start > len(cards) {
		start = len(cards)
	}
	if end > len(cards) {
		end = len(cards)
	}

	cards = cards[start:end]
	if 1 > len(cards) {
		blocks = []*Block{}
		return
	}

	var blockIDs []string
	for _, card := range cards {
		blockIDs = append(blockIDs, card.BlockID())
	}

	sqlBlocks := sql.GetBlocks(blockIDs)
	blocks = fromSQLBlocks(&sqlBlocks, "", 36)
	if 1 > len(blocks) {
		blocks = []*Block{}
		return
	}

	for i, b := range blocks {
		if nil == b {
			blocks[i] = &Block{
				ID:      blockIDs[i],
				Content: Conf.Language(180),
			}

			continue
		}

		b.RiffCardID = cards[i].ID()
		b.RiffCard = getRiffCard(cards[i].(*riff.FSRSCard).C)
	}
	return
}

func getRiffCard(card *fsrs.Card) *RiffCard {
	due := card.Due
	if due.IsZero() {
		due = time.Now()
	}

	return &RiffCard{
		Due:        due,
		Reps:       card.Reps,
		Lapses:     card.Lapses,
		State:      card.State,
		LastReview: card.LastReview,
	}
}

var (
	// reviewCardCache <cardID, card> caches cards during review so that undo can be supported.
	reviewCardCache = map[string]riff.Card{}

	// skipCardCache <cardID, card> caches skipped cards during review so that skip filtering can be
	// supported.
	skipCardCache = map[string]riff.Card{}
)

func ReviewFlashcard(deckID, cardID string, rating riff.Rating, reviewedCardIDs []string) (err error) {
	deckLock.Lock()
	defer deckLock.Unlock()

	waitForSyncingStorages()

	deck := Decks[deckID]
	card := deck.GetCard(cardID)
	if nil == card {
		return
	}

	if cachedCard := reviewCardCache[cardID]; nil != cachedCard {
		// A cache hit means this card has already been reviewed, so this review call is a re-review
		// after an undo. Restore the cached card back into the deck to reset it to the state before
		// the very first review.
		deck.SetCard(cachedCard)

		// Remove it from the skip cache (in case the last action was a skip); if it isn't in the skip
		// cache, the last action was a review, and removing it here is harmless anyway.
		delete(skipCardCache, cardID)
	} else {
		// First time reviewing this card, cache it so a subsequent undo-then-review is supported
		reviewCardCache[cardID] = card.Clone()
	}

	log := deck.Review(cardID, rating)
	if err = deck.Save(); err != nil {
		logging.LogErrorf("save deck [%s] failed: %s", deckID, err)
		return
	}

	if err = deck.SaveLog(log); err != nil {
		logging.LogErrorf("save review log [%s] failed: %s", deckID, err)
		return
	}

	_, unreviewedCount, _, _ := getDueFlashcards(deckID, reviewedCardIDs)
	if 1 > unreviewedCount {
		// No more cards left to review in this deck, meaning the last card has just been reviewed;
		// clear both the undo cache and the skip cache
		reviewCardCache = map[string]riff.Card{}
		skipCardCache = map[string]riff.Card{}
	}
	return
}

func SkipReviewFlashcard(deckID, cardID string) (err error) {
	deckLock.Lock()
	defer deckLock.Unlock()

	waitForSyncingStorages()

	deck := Decks[deckID]
	card := deck.GetCard(cardID)
	if nil == card {
		return
	}

	skipCardCache[cardID] = card
	return
}

type Flashcard struct {
	DeckID     string                 `json:"deckID"`
	CardID     string                 `json:"cardID"`
	BlockID    string                 `json:"blockID"`
	Lapses     int                    `json:"lapses"`
	Reps       int                    `json:"reps"`
	State      riff.State             `json:"state"`
	LastReview int64                  `json:"lastReview"`
	NextDues   map[riff.Rating]string `json:"nextDues"`
}

func newFlashcard(card riff.Card, deckID string, now time.Time) *Flashcard {
	nextDues := map[riff.Rating]string{}
	for rating, due := range card.NextDues() {
		nextDues[rating] = strings.TrimSpace(util.HumanizeDiffTime(due, now, Conf.Lang))
	}

	return &Flashcard{
		DeckID:     deckID,
		CardID:     card.ID(),
		BlockID:    card.BlockID(),
		Lapses:     card.GetLapses(),
		Reps:       card.GetReps(),
		State:      card.GetState(),
		LastReview: card.GetLastReview().UnixMilli(),
		NextDues:   nextDues,
	}
}

func GetNotebookDueFlashcards(boxID string, reviewedCardIDs []string) (ret []*Flashcard, unreviewedCount, unreviewedNewCardCount, unreviewedOldCardCount int, err error) {
	deckLock.Lock()
	defer deckLock.Unlock()

	waitForSyncingStorages()

	entries, err := os.ReadDir(filepath.Join(util.DataDir, boxID))
	if err != nil {
		logging.LogErrorf("read dir failed: %s", err)
		return
	}

	var rootIDs []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".sy") {
			continue
		}

		rootIDs = append(rootIDs, strings.TrimSuffix(entry.Name(), ".sy"))
	}

	var treeBlockIDs []string
	for _, rootID := range rootIDs {
		_, blockIDs := getTreeSubTreeChildBlocks(rootID)
		treeBlockIDs = append(treeBlockIDs, blockIDs...)
	}
	treeBlockIDs = gulu.Str.RemoveDuplicatedElem(treeBlockIDs)

	deck := Decks[builtinDeckID]
	if nil == deck {
		logging.LogWarnf("builtin deck not found")
		return
	}

	cards, unreviewedCnt, unreviewedNewCardCnt, unreviewedOldCardCnt := getDeckDueCards(deck, reviewedCardIDs, treeBlockIDs, Conf.Flashcard.NewCardLimit, Conf.Flashcard.ReviewCardLimit, Conf.Flashcard.ReviewMode)
	now := time.Now()
	for _, card := range cards {
		ret = append(ret, newFlashcard(card, builtinDeckID, now))
	}
	if 1 > len(ret) {
		ret = []*Flashcard{}
	}
	unreviewedCount = unreviewedCnt
	unreviewedNewCardCount = unreviewedNewCardCnt
	unreviewedOldCardCount = unreviewedOldCardCnt
	return
}

func GetTreeDueFlashcards(rootID string, reviewedCardIDs []string) (ret []*Flashcard, unreviewedCount, unreviewedNewCardCount, unreviewedOldCardCount int, err error) {
	deckLock.Lock()
	defer deckLock.Unlock()

	waitForSyncingStorages()

	deck := Decks[builtinDeckID]
	if nil == deck {
		return
	}

	_, treeBlockIDs := getTreeSubTreeChildBlocks(rootID)
	newCardLimit := Conf.Flashcard.NewCardLimit
	reviewCardLimit := Conf.Flashcard.ReviewCardLimit
	// Document-level new card/review card limit control https://github.com/siyuan-note/siyuan/issues/9365
	ial := sql.GetBlockAttrs(rootID)
	if newCardLimitStr := ial["custom-riff-new-card-limit"]; "" != newCardLimitStr {
		var convertErr error
		newCardLimit, convertErr = strconv.Atoi(newCardLimitStr)
		if nil != convertErr {
			logging.LogWarnf("invalid new card limit [%s]: %s", newCardLimitStr, convertErr)
		}
	}
	if reviewCardLimitStr := ial["custom-riff-review-card-limit"]; "" != reviewCardLimitStr {
		var convertErr error
		reviewCardLimit, convertErr = strconv.Atoi(reviewCardLimitStr)
		if nil != convertErr {
			logging.LogWarnf("invalid review card limit [%s]: %s", reviewCardLimitStr, convertErr)
		}
	}

	cards, unreviewedCnt, unreviewedNewCardCnt, unreviewedOldCardCnt := getDeckDueCards(deck, reviewedCardIDs, treeBlockIDs, newCardLimit, reviewCardLimit, Conf.Flashcard.ReviewMode)
	now := time.Now()
	for _, card := range cards {
		ret = append(ret, newFlashcard(card, builtinDeckID, now))
	}
	if 1 > len(ret) {
		ret = []*Flashcard{}
	}
	unreviewedCount = unreviewedCnt
	unreviewedNewCardCount = unreviewedNewCardCnt
	unreviewedOldCardCount = unreviewedOldCardCnt
	return
}

func getTreeSubTreeChildBlocks(rootID string) (treeBlockIDsMap map[string]bool, treeBlockIDs []string) {
	treeBlockIDsMap = map[string]bool{}
	root := treenode.GetBlockTree(rootID)
	if nil == root {
		return
	}
	if IsBoxDoc(root.BoxID, rootID) {
		return getBoxBlocks(root.BoxID)
	}

	bts := treenode.GetBlockTreesByPathPrefix(root.BoxID, strings.TrimSuffix(root.Path, ".sy"))
	for _, bt := range bts {
		treeBlockIDsMap[bt.ID] = true
		treeBlockIDs = append(treeBlockIDs, bt.ID)
	}
	return
}

func getTreeBlocks(rootID string) (treeBlockIDsMap map[string]bool, treeBlockIDs []string) {
	treeBlockIDsMap = map[string]bool{}
	bts := treenode.GetBlockTreesByRootID(rootID)
	for _, bt := range bts {
		treeBlockIDsMap[bt.ID] = true
		treeBlockIDs = append(treeBlockIDs, bt.ID)
	}
	return
}

func getBoxBlocks(boxID string) (blockIDsMap map[string]bool, blockIDs []string) {
	blockIDsMap = map[string]bool{}
	bts := treenode.GetBlockTreesByBoxID(boxID)
	for _, bt := range bts {
		blockIDsMap[bt.ID] = true
		blockIDs = append(blockIDs, bt.ID)
	}
	return
}

func GetDueFlashcards(deckID string, reviewedCardIDs []string) (ret []*Flashcard, unreviewedCount, unreviewedNewCardCount, unreviewedOldCardCount int, err error) {
	deckLock.Lock()
	defer deckLock.Unlock()

	waitForSyncingStorages()

	if "" == deckID {
		ret, unreviewedCount, unreviewedNewCardCount, unreviewedOldCardCount = getAllDueFlashcards(reviewedCardIDs)
		return
	}

	ret, unreviewedCount, unreviewedNewCardCount, unreviewedOldCardCount = getDueFlashcards(deckID, reviewedCardIDs)
	return
}

func getDueFlashcards(deckID string, reviewedCardIDs []string) (ret []*Flashcard, unreviewedCount, unreviewedNewCardCount, unreviewedOldCardCount int) {
	deck := Decks[deckID]
	if nil == deck {
		logging.LogWarnf("deck not found [%s]", deckID)
		return
	}

	cards, unreviewedCnt, unreviewedNewCardCnt, unreviewedOldCardCnt := getDeckDueCards(deck, reviewedCardIDs, nil, Conf.Flashcard.NewCardLimit, Conf.Flashcard.ReviewCardLimit, Conf.Flashcard.ReviewMode)
	now := time.Now()
	for _, card := range cards {
		ret = append(ret, newFlashcard(card, deckID, now))
	}
	if 1 > len(ret) {
		ret = []*Flashcard{}
	}
	unreviewedCount = unreviewedCnt
	unreviewedNewCardCount = unreviewedNewCardCnt
	unreviewedOldCardCount = unreviewedOldCardCnt
	return
}

func getAllDueFlashcards(reviewedCardIDs []string) (ret []*Flashcard, unreviewedCount, unreviewedNewCardCount, unreviewedOldCardCount int) {
	now := time.Now()
	for _, deck := range Decks {
		if deck.ID != builtinDeckID {
			// The Alt+0 flashcard review entry no longer returns deck flashcards
			// Alt+0 flashcard review entry no longer returns to card deck flashcards https://github.com/siyuan-note/siyuan/issues/10635
			continue
		}

		cards, unreviewedCnt, unreviewedNewCardCnt, unreviewedOldCardCnt := getDeckDueCards(deck, reviewedCardIDs, nil, Conf.Flashcard.NewCardLimit, Conf.Flashcard.ReviewCardLimit, Conf.Flashcard.ReviewMode)
		unreviewedCount += unreviewedCnt
		unreviewedNewCardCount += unreviewedNewCardCnt
		unreviewedOldCardCount += unreviewedOldCardCnt
		for _, card := range cards {
			ret = append(ret, newFlashcard(card, deck.ID, now))
		}
	}
	if 1 > len(ret) {
		ret = []*Flashcard{}
	}
	return
}

func (tx *Transaction) doRemoveFlashcards(operation *Operation) (ret *TxErr) {
	deckLock.Lock()
	defer deckLock.Unlock()

	if isSyncingStorages() {
		ret = &TxErr{code: TxErrCodeDataIsSyncing}
		return
	}

	deckID := operation.DeckID
	blockIDs := operation.BlockIDs

	if err := tx.removeBlocksDeckAttr(blockIDs, deckID); err != nil {
		return &TxErr{code: TxErrCodeWriteTree, msg: err.Error(), id: deckID}
	}

	if "" == deckID { // support removing flashcards from the All deck https://github.com/siyuan-note/siyuan/issues/7425
		for _, deck := range Decks {
			removeFlashcardsByBlockIDs(blockIDs, deck)
		}
	} else {
		removeFlashcardsByBlockIDs(blockIDs, Decks[deckID])
	}
	return
}

func (tx *Transaction) removeBlocksDeckAttr(blockIDs []string, deckID string) (err error) {
	var rootIDs []string
	blockRoots := map[string]string{}
	for _, blockID := range blockIDs {
		bt := treenode.GetBlockTree(blockID)
		if nil == bt {
			continue
		}
		// Encrypted notebooks don't support flashcards, so skip them on removal too, to avoid a
		// needless write to the encrypted tree
		if IsEncryptedBox(bt.BoxID) {
			continue
		}

		rootIDs = append(rootIDs, bt.RootID)
		blockRoots[blockID] = bt.RootID
	}
	rootIDs = gulu.Str.RemoveDuplicatedElem(rootIDs)

	trees := map[string]*parse.Tree{}
	for _, blockID := range blockIDs {
		rootID := blockRoots[blockID]

		tree := trees[rootID]
		if nil == tree {
			tree, _ = tx.loadTree(blockID)
		}
		if nil == tree {
			continue
		}
		trees[rootID] = tree

		node := treenode.GetNodeInTree(tree, blockID)
		if nil == node {
			continue
		}

		oldAttrs := parse.IAL2Map(node.KramdownIAL)

		deckAttrs := node.IALAttr(NodeAttrRiffDecks)
		var deckIDs []string
		if "" != deckID {
			availableDeckIDs := getDeckIDs()
			for dID := range strings.SplitSeq(deckAttrs, ",") {
				if dID != deckID && gulu.Str.Contains(dID, availableDeckIDs) {
					deckIDs = append(deckIDs, dID)
				}
			}
		}

		deckIDs = gulu.Str.RemoveDuplicatedElem(deckIDs)
		val := strings.Join(deckIDs, ",")
		val = strings.TrimPrefix(val, ",")
		val = strings.TrimSuffix(val, ",")
		if "" == val {
			node.RemoveIALAttr(NodeAttrRiffDecks)
		} else {
			node.SetIALAttr(NodeAttrRiffDecks, val)
		}

		tx.writeTree(tree)

		cache.PutBlockIALInBox(blockID, tree.Box, parse.IAL2Map(node.KramdownIAL))
		pushBlockAttrs(oldAttrs, node)
	}

	return
}

func removeFlashcardsByBlockIDs(blockIDs []string, deck *riff.Deck) {
	if nil == deck {
		logging.LogErrorf("deck is nil")
		return
	}

	cards := deck.GetCardsByBlockIDs(blockIDs)
	if 1 > len(cards) {
		return
	}

	for _, card := range cards {
		deck.RemoveCard(card.ID())
	}
	err := deck.Save()
	if err != nil {
		logging.LogErrorf("save deck [%s] failed: %s", deck.ID, err)
	}
}

func (tx *Transaction) doAddFlashcards(operation *Operation) (ret *TxErr) {
	deckLock.Lock()
	defer deckLock.Unlock()

	if isSyncingStorages() {
		ret = &TxErr{code: TxErrCodeDataIsSyncing}
		return
	}

	deckID := operation.DeckID
	blockIDs := operation.BlockIDs

	foundDeck := false
	for _, deck := range Decks {
		if deckID == deck.ID {
			foundDeck = true
			break
		}
	}
	if !foundDeck {
		deck, createErr := createDeck0("Built-in Deck", builtinDeckID)
		if nil == createErr {
			Decks[deck.ID] = deck
		}
	}

	blockRoots := map[string]string{}
	for _, blockID := range blockIDs {
		bt := treenode.GetBlockTree(blockID)
		if nil == bt {
			continue
		}
		// Flashcards are scheduled and stored globally, and encrypted notebooks don't support
		// flashcards; skip their blocks to avoid persisting a plaintext review schedule into the
		// global riff storage
		if IsEncryptedBox(bt.BoxID) {
			continue
		}

		blockRoots[blockID] = bt.RootID
	}

	trees := map[string]*parse.Tree{}
	for _, blockID := range blockIDs {
		rootID := blockRoots[blockID]

		tree := trees[rootID]
		if nil == tree {
			tree, _ = tx.loadTree(blockID)
		}
		if nil == tree {
			continue
		}
		trees[rootID] = tree

		node := treenode.GetNodeInTree(tree, blockID)
		if nil == node {
			continue
		}

		oldAttrs := parse.IAL2Map(node.KramdownIAL)

		deckAttrs := node.IALAttr(NodeAttrRiffDecks)
		deckIDs := strings.Split(deckAttrs, ",")
		deckIDs = append(deckIDs, deckID)
		deckIDs = gulu.Str.RemoveDuplicatedElem(deckIDs)
		val := strings.Join(deckIDs, ",")
		val = strings.TrimPrefix(val, ",")
		val = strings.TrimSuffix(val, ",")
		node.SetIALAttr(NodeAttrRiffDecks, val)

		tx.writeTree(tree)

		cache.PutBlockIALInBox(blockID, tree.Box, parse.IAL2Map(node.KramdownIAL))
		pushBlockAttrs(oldAttrs, node)
	}

	deck := Decks[deckID]
	if nil == deck {
		logging.LogWarnf("deck [%s] not found", deckID)
		return
	}

	for _, blockID := range blockIDs {
		cards := deck.GetCardsByBlockID(blockID)
		if 0 < len(cards) {
			// Only one flashcard can be generated per block https://github.com/siyuan-note/siyuan/issues/7476
			continue
		}

		deck.AddCard(ast.NewNodeID(), blockID)
	}

	if err := deck.Save(); err != nil {
		logging.LogErrorf("save deck [%s] failed: %s", deckID, err)
		return
	}
	return
}

func LoadFlashcards() {
	riffSavePath := getRiffDir()
	if err := os.MkdirAll(riffSavePath, 0755); err != nil {
		logging.LogErrorf("create riff dir [%s] failed: %s", riffSavePath, err)
		return
	}

	Decks = map[string]*riff.Deck{}

	entries, err := os.ReadDir(riffSavePath)
	if err != nil {
		logging.LogErrorf("read riff dir failed: %s", err)
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if before, ok := strings.CutSuffix(name, ".deck"); ok {
			deckID := before
			deck, loadErr := riff.LoadDeck(riffSavePath, deckID, Conf.Flashcard.RequestRetention, Conf.Flashcard.MaximumInterval, Conf.Flashcard.Weights)
			if nil != loadErr {
				logging.LogErrorf("load deck [%s] failed: %s", name, loadErr)
				continue
			}

			if 0 == deck.Created {
				deck.Created = time.Now().Unix()
			}
			if 0 == deck.Updated {
				deck.Updated = deck.Created
			}

			Decks[deckID] = deck
		}
	}
}

const builtinDeckID = "20230218211946-2kw8jgx"

func RenameDeck(deckID, name string) (err error) {
	deckLock.Lock()
	defer deckLock.Unlock()

	waitForSyncingStorages()

	deck := Decks[deckID]
	deck.Name = name
	err = deck.Save()
	if err != nil {
		logging.LogErrorf("save deck [%s] failed: %s", deckID, err)
		return
	}
	return
}

func RemoveDeck(deckID string) (err error) {
	deckLock.Lock()
	defer deckLock.Unlock()

	waitForSyncingStorages()

	riffSavePath := getRiffDir()
	deckPath := filepath.Join(riffSavePath, deckID+".deck")
	if filelock.IsExist(deckPath) {
		if err = filelock.Remove(deckPath); err != nil {
			return
		}
	}

	cardsPath := filepath.Join(riffSavePath, deckID+".cards")
	if filelock.IsExist(cardsPath) {
		if err = filelock.Remove(cardsPath); err != nil {
			return
		}
	}

	LoadFlashcards()
	return
}

func CreateDeck(name string) (deck *riff.Deck, err error) {
	deckLock.Lock()
	defer deckLock.Unlock()
	return createDeck(name)
}

func createDeck(name string) (deck *riff.Deck, err error) {
	waitForSyncingStorages()

	deckID := ast.NewNodeID()
	deck, err = createDeck0(name, deckID)
	return
}

func createDeck0(name string, deckID string) (deck *riff.Deck, err error) {
	riffSavePath := getRiffDir()
	deck, err = riff.LoadDeck(riffSavePath, deckID, Conf.Flashcard.RequestRetention, Conf.Flashcard.MaximumInterval, Conf.Flashcard.Weights)
	if err != nil {
		logging.LogErrorf("load deck [%s] failed: %s", deckID, err)
		return
	}
	deck.Name = name
	Decks[deckID] = deck
	err = deck.Save()
	if err != nil {
		logging.LogErrorf("save deck [%s] failed: %s", deckID, err)
		return
	}
	return
}

func GetDecks() (decks []*riff.Deck) {
	deckLock.Lock()
	defer deckLock.Unlock()

	for _, deck := range Decks {
		if deck.ID == builtinDeckID {
			continue
		}

		decks = append(decks, deck)
	}
	if 1 > len(decks) {
		decks = []*riff.Deck{}
	}

	sort.Slice(decks, func(i, j int) bool {
		return decks[i].Updated > decks[j].Updated
	})
	return
}

func getRiffDir() string {
	return filepath.Join(util.DataDir, "storage", "riff")
}

func getDeckIDs() (deckIDs []string) {
	for deckID := range Decks {
		deckIDs = append(deckIDs, deckID)
	}
	return
}

func getDeckDueCards(deck *riff.Deck, reviewedCardIDs, blockIDs []string, newCardLimit, reviewCardLimit, reviewMode int) (ret []riff.Card, unreviewedCount, unreviewedNewCardCountInRound, unreviewedOldCardCountInRound int) {
	ret = []riff.Card{}
	var retNew, retOld []riff.Card

	dues := deck.Dues()
	toChecks := map[string]riff.Card{}
	for _, c := range dues {
		if 0 < len(blockIDs) && !gulu.Str.Contains(c.BlockID(), blockIDs) {
			continue
		}

		toChecks[c.BlockID()] = c
	}
	var toCheckBlockIDs []string
	var tmp []riff.Card
	for bID := range toChecks {
		toCheckBlockIDs = append(toCheckBlockIDs, bID)
	}
	checkResult := treenode.ExistBlockTrees(toCheckBlockIDs)
	for bID, exists := range checkResult {
		if exists {
			tmp = append(tmp, toChecks[bID])
		}
	}
	dues = tmp

	reviewedCardCount := len(reviewedCardIDs)
	if 1 > reviewedCardCount {
		// No reviewed card IDs were passed in, meaning a new review session is starting; clear the caches
		reviewCardCache = map[string]riff.Card{}
		skipCardCache = map[string]riff.Card{}
	}

	newCount := 0
	reviewCount := 0
	for _, reviewedCard := range reviewCardCache {
		if riff.New == reviewedCard.GetState() {
			newCount++
		} else {
			reviewCount++
		}
	}

	for _, c := range dues {
		if nil != skipCardCache[c.ID()] {
			continue
		}

		if 0 < len(reviewedCardIDs) {
			if !gulu.Str.Contains(c.ID(), reviewedCardIDs) {
				unreviewedCount++
				if riff.New == c.GetState() {
					if newCount < newCardLimit {
						unreviewedNewCardCountInRound++
					}
				} else {
					if reviewCount < reviewCardLimit {
						unreviewedOldCardCountInRound++
					}
				}
			}
		} else {
			unreviewedCount++
			if riff.New == c.GetState() {
				if newCount < newCardLimit {
					unreviewedNewCardCountInRound++
				}
			} else {
				if reviewCount < reviewCardLimit {
					unreviewedOldCardCountInRound++
				}
			}
		}

		if riff.New == c.GetState() {
			if newCount >= newCardLimit {
				continue
			}

			newCount++
			retNew = append(retNew, c)
		} else {
			if reviewCount >= reviewCardLimit {
				continue
			}

			reviewCount++
			retOld = append(retOld, c)
		}

		ret = append(ret, c)
	}

	switch reviewMode {
	case 1: // review new cards first
		ret = nil
		ret = append(ret, retNew...)
		ret = append(ret, retOld...)
	case 2: // review old cards first
		ret = nil
		ret = append(ret, retOld...)
		ret = append(ret, retNew...)
	}
	return
}

const (
	NodeAttrRiffDecks = "custom-riff-decks"
)
