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
	"container/heap"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/88250/gulu"
	ignore "github.com/sabhiram/go-gitignore"
	"github.com/siyuan-note/eventbus"
	"github.com/siyuan-note/logging"
	"github.com/siyuan-note/siyuan/kernel/sql"
	"github.com/siyuan-note/siyuan/kernel/task"
	"github.com/siyuan-note/siyuan/kernel/util"
)

const (
	embeddingBatchSize      = 10
	embeddingMaxConcurrency = 8
	embeddingMinTextLen     = 7
	embeddingMaxContentLen  = 12000
	embeddingVectorDim      = 4 // float32 = 4 bytes

	// Backoff parameters for retrying failed embeddings: avoid a connection storm when the API is unavailable
	// delay = min(embeddingBackoffBase << (failCount-1), embeddingBackoffMax)
	embeddingBackoffBase  = 30      // Retry 30s after the first failure
	embeddingBackoffMax   = 30 * 60 // Upper bound of 30 minutes
	embeddingMaxFailCount = 8       // Once a single block fails this many times in a row, it is treated as a permanent failure and no longer scheduled

	// Values of block_embeddings.ignored_type: distinguish why a block was skipped and not embedded
	embeddingIgnoredNone   = 0 // Not ignored (embedded normally, or currently retrying after failure)
	embeddingIgnoredByLen  = 1 // Content length out of range (< 7 or > 12000 characters)
	embeddingIgnoredByConf = 2 // Matched by the .siyuan/embeddingignore config
)

var (
	embeddingDirtyCh = make(chan string, 1024)
	embeddingTableOk bool

	embeddingIgnoreLoaded  bool
	embeddingIgnoreMatcher *ignore.GitIgnore
	embeddingIgnoreLock    sync.Mutex

	embeddingStop atomic.Bool

	// embeddingErrNotified marks whether the user has already been notified of an embedding failure this round, to
	// avoid popping up the message repeatedly when multiple concurrent workers fail.
	// It is reset together with embeddingStop each time processPendingEmbeddings starts.
	embeddingErrNotified atomic.Bool

	// embeddingIndexerRunning marks whether the background indexer's infinite loop is already running, to prevent
	// fullReindexEmbedding from starting multiple goroutines.
	// If embedding is not enabled at startup, StartEmbeddingIndexer returns immediately and this flag stays false;
	// it is used later to decide whether to start the loop once the user enables embedding and triggers a rebuild.
	embeddingIndexerRunning atomic.Bool
)

func checkEmbeddingTable() bool {
	_, err := sql.QueryNoLimit("SELECT COUNT(*) FROM block_embeddings")
	if err != nil {
		logging.LogWarnf("block_embeddings table not available, embedding indexer disabled: %s", err)
		return false
	}
	return true
}

func StartEmbeddingIndexer() {
	if !checkEmbeddingTable() || !isEmbeddingEnabled() {
		return
	}

	// CAS prevents starting twice: if the infinite loop is already running (e.g. triggered by the rebuild button),
	// return immediately to avoid registering the subscriber again and starting multiple goroutines
	if !embeddingIndexerRunning.CompareAndSwap(false, true) {
		return
	}

	eventbus.Subscribe(eventbus.EvtEmbeddingDirty, func(id string) {
		select {
		case embeddingDirtyCh <- id:
		default:
		}
	})

	embeddingTableOk = true

	processPendingEmbeddings()

	for {
		select {
		case <-embeddingDirtyCh:
			processPendingEmbeddings()
		case <-time.After(30 * time.Second):
			processPendingEmbeddings()
		}
	}
}

// PrepareEmbeddingSearch only checks the table and configuration and sets embeddingTableOk to true; it does not
// start the background indexing loop.
// This is for one-shot CLI commands (e.g. search -m 4): StartEmbeddingIndexer runs an infinite loop and cannot be
// used directly by a process that exits immediately.
func PrepareEmbeddingSearch() {
	if checkEmbeddingTable() && isEmbeddingEnabled() {
		embeddingTableOk = true
	}
}

type embeddingJob struct {
	texts  []string
	blocks []map[string]any
}

func processPendingEmbeddings() {
	if !isEmbeddingEnabled() {
		return
	}

	embeddingStop.Store(false)
	embeddingErrNotified.Store(false)

	workCh := make(chan embeddingJob, embeddingMaxConcurrency*2)

	var workersWg sync.WaitGroup
	for range embeddingMaxConcurrency {
		workersWg.Go(func() {
			for job := range workCh {
				if embeddingStop.Load() {
					// This round has already been tripped (triggered by another worker's failure); the blocks in
					// these backlogged jobs cannot simply be dropped, otherwise they would still be e.id IS NULL and
					// keep getting pulled up again next round without ever having a row written. Handle them as
					// failures and write placeholder rows.
					recordFailedEmbedding(job.blocks, "round stopped due to earlier failure in this round")
					continue
				}
				doEmbedAndStore(job.texts, job.blocks)
			}
		})
	}

	go func() {
		defer close(workCh)
		for {
			if embeddingStop.Load() {
				return
			}

			// The SQL coarse filter uses the minimum backoff interval as the lower bound, to make sure no due retry
			// block is missed; the precise per-block backoff is checked again below on the Go side
			now := time.Now().Unix()
			cutoff := now - int64(embeddingBackoffBase) // embeddingBackoffBase is in seconds
			results, err := sql.QueryNoLimitArgs(stmtPendingBlocks, embeddingMaxFailCount, cutoff)
			if err != nil {
				logging.LogErrorf("query pending embedding blocks failed: %s", err)
				return
			}

			if 1 > len(results) {
				return
			}

			var texts []string
			var blocks []map[string]any
			anySubmitted := false                      // Whether any job was submitted to workCh this round
			backoffSkipped := 0                        // Number of blocks skipped because their backoff time hasn't elapsed (their state is unchanged, so they'll be pulled up again next round)
			minRemaining := int64(embeddingBackoffMax) // The smallest remaining wait time in seconds among these blocks (embeddingBackoffMax is in seconds)
			for _, row := range results {
				id, _ := row["id"].(string)
				rootID, _ := row["root_id"].(string)
				box, _ := row["box"].(string)
				path, _ := row["path"].(string)
				updated, _ := row["updated"].(string)
				content, _ := row["content"].(string)

				// For a block that has failed before, precisely check whether its backoff time has elapsed based on
				// its own fail_count; skip it this round if not yet due
				failCount, _ := row["fail_count"].(int64)
				lastTried, _ := row["last_tried"].(int64)
				if failCount > 0 {
					if failCount >= embeddingMaxFailCount {
						continue // Permanent failure, no longer scheduled
					}
					required := int64(embeddingBackoffFor(int(failCount)) / time.Second)
					if elapsed := now - lastTried; elapsed < required {
						backoffSkipped++
						if remaining := required - elapsed; remaining < minRemaining {
							minRemaining = remaining
						}
						continue // This block's backoff time hasn't elapsed yet
					}
				}

				matcher := getEmbeddingIgnoreMatcher()
				if nil != matcher && matcher.MatchesPath("/"+box+path) {
					// Matched by the .siyuan/embeddingignore config; config-based ignoring takes priority over length-based ignoring
					sql.Exec("INSERT OR IGNORE INTO block_embeddings (id, root_id, box, path, embedding, model, content_len, updated, fail_count, last_tried, ignored_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?)",
						id, rootID, box, path, []byte{}, embeddingModel(), 0, updated, embeddingIgnoredByConf)
					continue
				}
				if len(content) < embeddingMinTextLen || len(content) > embeddingMaxContentLen {
					// Content length out of range (too short or too long): ignored by length
					sql.Exec("INSERT OR IGNORE INTO block_embeddings (id, root_id, box, path, embedding, model, content_len, updated, fail_count, last_tried, ignored_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?)",
						id, rootID, box, path, []byte{}, embeddingModel(), 0, updated, embeddingIgnoredByLen)
					continue
				}
				row["plain_text"] = content
				texts = append(texts, content)
				blocks = append(blocks, row)

				if len(texts) >= embeddingBatchSize {
					workCh <- embeddingJob{texts: texts, blocks: blocks}
					anySubmitted = true
					texts = nil
					blocks = nil
				}
			}
			if len(texts) > 0 {
				workCh <- embeddingJob{texts: texts, blocks: blocks}
				anySubmitted = true
			}

			// No job was submitted this round, and all blocks were skipped due to backoff: their state is unchanged,
			// so the next round's SQL will pull up the same blocks again. Going straight to the next round would
			// cause CPU busy-waiting plus high-frequency DB queries. Sleep until the nearest due time before
			// continuing, checking the stop flag along the way so we can exit promptly if tripped.
			if !anySubmitted && backoffSkipped > 0 {
				wait := max(time.Duration(minRemaining)*time.Second, time.Second)
				// Sleep in small steps, checking embeddingStop once per second, to exit as soon as possible if tripped
				for wait > 0 && !embeddingStop.Load() {
					step := min(wait, time.Second)
					time.Sleep(step)
					wait -= step
				}
			}
		}
	}()

	workersWg.Wait()
}

// stmtPendingBlocks pulls up blocks pending embedding, in two categories:
//  1. Never attempted (e.id IS NULL);
//  2. Previously failed, not yet at the permanent-failure threshold, and past the backoff interval since the last
//     attempt (e.last_tried < ?).
//
// Parameter order: ?1=maxFailCount, ?2=now-backoff (only a coarse lower bound for blocks with fail_count>0; the
// precise backoff is computed on the Go side per block's fail_count).
const stmtPendingBlocks = "SELECT b.id, b.root_id, b.box, b.path, b.content, b.updated, " +
	"COALESCE(e.fail_count, 0) AS fail_count, COALESCE(e.last_tried, 0) AS last_tried " +
	"FROM blocks b " +
	"LEFT JOIN block_embeddings e ON b.id = e.id " +
	"WHERE e.id IS NULL " +
	"OR (e.fail_count > 0 AND e.fail_count < ? AND e.last_tried < ?) " +
	"ORDER BY fail_count ASC, b.updated DESC LIMIT 100"

// embeddingBackoffFor returns the backoff interval for a given failure count (the first failure, fail_count=1,
// corresponds to base).
func embeddingBackoffFor(failCount int) time.Duration {
	if failCount < 1 {
		return time.Duration(embeddingBackoffBase) * time.Second
	}
	shift := min(failCount-1,
		// Prevent overflow
		20)
	d := embeddingBackoffBase << uint(shift)
	if d > embeddingBackoffMax || d < 0 {
		return time.Duration(embeddingBackoffMax) * time.Second
	}
	return time.Duration(d) * time.Second
}

func encodeVector(vec []float32) []byte {
	buf := make([]byte, len(vec)*embeddingVectorDim)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*embeddingVectorDim:], math.Float32bits(v))
	}
	return buf
}

func decodeVector(b []byte) []float32 {
	if len(b) == 0 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), len(b)/embeddingVectorDim)
}

// recordFailedEmbedding marks a batch of blocks as failed (fail_count+1, writes an empty embedding), trips this
// round, and notifies the user.
// Used as the unified failure handling when an API call errors out or returns a vector count that doesn't match the input.
func recordFailedEmbedding(blocks []map[string]any, reason string) {
	embeddingStop.Store(true)
	logging.LogErrorf("create embeddings failed (%s), stop this round", reason)
	// Multiple workers may fail concurrently; use CAS to ensure the user is notified only once this round
	if embeddingErrNotified.CompareAndSwap(false, true) {
		util.PushErrMsg("Embedding request failed, indexing paused. Please check AI embedding config.", 5000)
	}

	now := time.Now().Unix()
	for _, row := range blocks {
		id, _ := row["id"].(string)
		rootID, _ := row["root_id"].(string)
		box, _ := row["box"].(string)
		path, _ := row["path"].(string)
		updated, _ := row["updated"].(string)
		// First make sure the placeholder row exists (INSERT OR IGNORE does not overwrite an existing row), then increment the failure count
		sql.Exec("INSERT OR IGNORE INTO block_embeddings (id, root_id, box, path, embedding, model, content_len, updated, fail_count, last_tried, ignored_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0)",
			id, rootID, box, path, []byte{}, embeddingModel(), 0, updated)
		sql.Exec("UPDATE block_embeddings SET fail_count = fail_count + 1, last_tried = ?, embedding = ?, model = ?, content_len = 0, ignored_type = 0 WHERE id = ?",
			now, []byte{}, embeddingModel(), id)
	}
}

func doEmbedAndStore(texts []string, blocks []map[string]any) {
	vectors, err := util.BatchGetEmbeddings(texts, embeddingKey(), embeddingBaseURL(), embeddingModel(), embeddingDimensions(), embeddingTimeout())
	if err != nil {
		// Any API error (including a nonexistent model, auth failure, rate limiting, or network issues) trips this
		// round, to avoid a connection storm
		recordFailedEmbedding(blocks, err.Error())
		return
	}

	// Some OpenAI-compatible APIs deduplicate repeated inputs and return fewer vectors than the input count. In that
	// case alignment is impossible, so treat the whole batch as failed to avoid an out-of-bounds panic
	if len(vectors) != len(blocks) {
		recordFailedEmbedding(blocks, fmt.Sprintf("count mismatch: requested %d but got %d", len(blocks), len(vectors)))
		return
	}

	for i, row := range blocks {
		id, _ := row["id"].(string)
		rootID, _ := row["root_id"].(string)
		box, _ := row["box"].(string)
		path, _ := row["path"].(string)
		updated, _ := row["updated"].(string)
		plainText, _ := row["plain_text"].(string)

		buf := encodeVector(vectors[i])

		// On success, rewrite the whole row, resetting fail_count/last_tried/ignored_type to 0
		err = sql.Exec("INSERT OR REPLACE INTO block_embeddings (id, root_id, box, path, embedding, model, content_len, updated, fail_count, last_tried, ignored_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0, 0)",
			id, rootID, box, path, buf, embeddingModel(), len(plainText), updated)
		if err != nil {
			logging.LogErrorf("store embedding failed for block [%s]: %s", id, err)
		}
	}
}

func getEmbeddingIgnoreMatcher() *ignore.GitIgnore {
	if embeddingIgnoreLoaded {
		return embeddingIgnoreMatcher
	}

	embeddingIgnoreLock.Lock()
	defer embeddingIgnoreLock.Unlock()

	if embeddingIgnoreLoaded {
		return embeddingIgnoreMatcher
	}

	embeddingIgnorePath := filepath.Join(util.DataDir, ".siyuan", "embeddingignore")
	if !gulu.File.IsExist(embeddingIgnorePath) {
		return nil // Do not set the loaded flag when the file doesn't exist, so it can be reloaded once the user creates it later
	}

	data, err := os.ReadFile(embeddingIgnorePath)
	if err != nil {
		logging.LogErrorf("read embeddingignore [%s] failed: %s", embeddingIgnorePath, err)
		return nil // Also don't set the flag when the read fails, so the next call retries
	}

	dataStr := string(data)
	dataStr = strings.ReplaceAll(dataStr, "\r\n", "\n")
	lines := strings.Split(dataStr, "\n")

	embeddingIgnoreMatcher = ignore.CompileIgnoreLines(lines...)
	embeddingIgnoreLoaded = true // Only set the flag after a successful load, so a file created later isn't left permanently unloaded
	return embeddingIgnoreMatcher
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)))
}

type scoredBlock struct {
	id    string
	score float32
}

type scoredHeap []scoredBlock

func (h scoredHeap) Len() int           { return len(h) }
func (h scoredHeap) Less(i, j int) bool { return h[i].score < h[j].score } // min-heap
func (h scoredHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *scoredHeap) Push(x any) {
	*h = append(*h, x.(scoredBlock))
}
func (h *scoredHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func SemanticSearchBlock(query string, boxes, paths []string, types, subTypes map[string]bool, page, pageSize int) (blocks []*Block, matchedBlockCount, matchedRootCount, pageCount int) {
	blocks = []*Block{}

	if !embeddingTableOk || !isEmbeddingEnabled() || "" == query {
		return
	}

	vectors, err := util.BatchGetEmbeddings([]string{query}, embeddingKey(), embeddingBaseURL(), embeddingModel(), embeddingDimensions(), embeddingTimeout())
	if err != nil || 1 > len(vectors) {
		logging.LogErrorf("get query embedding failed")
		return
	}
	queryVec := vectors[0]

	boxFilter, boxArgs := buildBoxesFilter(boxes, "be.")
	pathFilter, pathArgs := buildPathsFilter(paths, "be.")
	boxDocFilter, boxDocArgs := buildRootIDExclusionFilter(hiddenBoxDocRootIDs(), "b.")
	typeFilter := buildTypeFilter(types, subTypes, "b.")
	hasFilter := 0 < len(boxes) || 0 < len(paths) || 0 < len(types) || "" != boxDocFilter
	hasTypeFilter := 0 < len(types)

	numWorkers := max(runtime.GOMAXPROCS(0), 1)

	// Number of vector-recall candidates: when reranking is enabled, always recall a fixed candidateCount so all
	// pages are based on the same candidate set; otherwise only fetch what the current page needs.
	topK := page * pageSize
	if isRerankEnabled() {
		topK = rerankCandidateCount()
	}
	h := &scoredHeap{}
	heap.Init(h)

	scanSize := 4096
	cursor := int64(0)

	for {
		var q string
		var args []any
		if hasFilter {
			q = fmt.Sprintf("SELECT be.rowid, be.id, be.embedding FROM block_embeddings be JOIN blocks b ON be.id = b.id WHERE be.embedding IS NOT NULL AND length(be.embedding) > 0 AND be.rowid > %d", cursor)
			if hasTypeFilter {
				q += " AND " + typeFilter
			}
			q += boxFilter + pathFilter + boxDocFilter
			// Filter values are passed as bound parameters to avoid SQL concatenation injection
			args = append(append(append([]any{}, boxArgs...), pathArgs...), boxDocArgs...)
			q += fmt.Sprintf(" ORDER BY be.rowid LIMIT %d", scanSize)
		} else {
			q = fmt.Sprintf("SELECT rowid, id, embedding FROM block_embeddings WHERE embedding IS NOT NULL AND length(embedding) > 0 AND rowid > %d ORDER BY rowid LIMIT %d", cursor, scanSize)
		}
		rows, qErr := sql.QueryNoLimitArgs(q, args...)
		if qErr != nil {
			logging.LogErrorf("query embeddings for search failed: %s", qErr)
			break
		}
		if 1 > len(rows) {
			break
		}

		rawCursor, _ := rows[len(rows)-1]["rowid"].(int64)
		if rawCursor > cursor {
			cursor = rawCursor
		}

		chunkSize := (len(rows) + numWorkers - 1) / numWorkers
		scoredCh := make(chan []scoredBlock, numWorkers)
		var wg sync.WaitGroup

		for w := 0; w < numWorkers; w++ {
			start := w * chunkSize
			end := min(start+chunkSize, len(rows))
			if start >= end {
				continue
			}

			wg.Add(1)
			go func(chunk []map[string]any) {
				defer wg.Done()
				local := make([]scoredBlock, 0, len(chunk))
				for _, row := range chunk {
					embRaw := row["embedding"].([]byte)
					if len(embRaw) == 0 {
						continue
					}
					buf := make([]byte, len(embRaw))
					copy(buf, embRaw)
					vec := decodeVector(buf)
					score := cosineSimilarity(queryVec, vec)
					id, _ := row["id"].(string)
					local = append(local, scoredBlock{id: id, score: score})
				}
				scoredCh <- local
			}(rows[start:end])
		}

		wg.Wait()
		close(scoredCh)

		for ch := range scoredCh {
			for _, s := range ch {
				if h.Len() < topK {
					heap.Push(h, s)
				} else if s.score > (*h)[0].score {
					heap.Pop(h)
					heap.Push(h, s)
				}
			}
		}
	}

	matchedBlockCount = h.Len()
	if 1 > matchedBlockCount {
		pageCount = 0
		return
	}

	result := make([]scoredBlock, h.Len())
	for i := len(result) - 1; i >= 0; i-- {
		result[i] = heap.Pop(h).(scoredBlock)
	}

	// Extract all candidate block IDs sorted by vector similarity descending. When reranking is enabled, result is
	// already the fixed candidateCount; when disabled, result is exactly what the current page needs, and the
	// pagination logic below handles both cases uniformly.
	var candidateIDs []string
	for _, s := range result {
		candidateIDs = append(candidateIDs, s.id)
	}

	sqlBlocks := sql.GetBlocks(candidateIDs)

	// Rerank: precisely re-sort by scoring the query against each candidate block's text pairwise; on failure, fall
	// back to the original vector-similarity order without blocking the search.
	// Note that the order returned by GetBlocks does not necessarily match candidateIDs, so reranking is based on
	// the returned sqlBlocks order.
	sqlBlocks = rerankSqlBlocks(query, sqlBlocks)

	offset := (page - 1) * pageSize
	if offset >= len(sqlBlocks) {
		pageCount = (matchedBlockCount + pageSize - 1) / pageSize
		return
	}

	end := min(offset+pageSize, len(sqlBlocks))

	rootIDSet := map[string]bool{}
	for i := offset; i < end; i++ {
		b := sqlBlocks[i]
		rootIDSet[b.RootID] = true
		blocks = append(blocks, fromSQLBlock(b, "", 36))
	}
	matchedRootCount = len(rootIDSet)
	pageCount = (matchedBlockCount + pageSize - 1) / pageSize

	return
}

func isEmbeddingEnabled() bool {
	return nil != Conf.AI.Embedding && Conf.AI.Embedding.Enabled && len(Conf.AI.Embedding.APIKey) > 0
}

// rerankSqlBlocks uses the rerank model to precisely re-sort candidate blocks against the query pairwise. If
// reranking is disabled or the call fails, it returns the blocks unchanged (falling back to vector similarity order).
// The rerank service uses each block's Content (the plain text the embedding vector represents) as the document
// text, so ordering is consistent across pages: rerank scores each query-doc pair independently, and the score does
// not change with the candidate set's size.
func rerankSqlBlocks(query string, sqlBlocks []*sql.Block) []*sql.Block {
	if !isRerankEnabled() || len(sqlBlocks) < 2 {
		return sqlBlocks
	}

	documents := make([]string, len(sqlBlocks))
	for i, b := range sqlBlocks {
		documents[i] = b.Content
	}

	// topN=0 means top_n is not passed, requiring the server to return scores for all documents, so results aren't truncated by a server-side top_n cap
	indices, _, err := util.Rerank(query, documents, rerankKey(), rerankEndpoint(), rerankModel(), 0, rerankTimeout())
	if nil != err {
		logging.LogErrorf("rerank failed, fallback to vector similarity order: %s", err)
		return sqlBlocks
	}
	if len(indices) != len(sqlBlocks) {
		// The count returned by the server doesn't match the input; fall back to the original order to avoid misalignment
		logging.LogErrorf("rerank returned %d indices for %d documents, fallback", len(indices), len(sqlBlocks))
		return sqlBlocks
	}

	// Guard against duplicate indices: the server shouldn't return duplicate indices, but if it does, fall back to
	// avoid some blocks being lost while others are duplicated
	seen := make(map[int]bool, len(indices))
	for _, idx := range indices {
		if seen[idx] {
			logging.LogErrorf("rerank returned duplicate index %d, fallback", idx)
			return sqlBlocks
		}
		seen[idx] = true
	}

	reranked := make([]*sql.Block, len(indices))
	for i, idx := range indices {
		reranked[i] = sqlBlocks[idx]
	}
	return reranked
}

// ReindexEmbedding clears the embedding vector table and triggers the background indexer to recompute all blocks.
// It runs asynchronously: it returns immediately after queueing the task.
func ReindexEmbedding() {
	task.AppendTask(task.DatabaseIndexEmbeddingFull, fullReindexEmbedding)
}

// fullReindexEmbedding is the actual rebuild logic, scheduled and executed by the task queue.
// It only DELETEs data rows and keeps the table structure (it cannot DROP, since DROP would also trigger recreating
// blocks and all other tables); once cleared, every block satisfies e.id IS NULL, so the resident indexer will
// automatically re-embed everything on its next round.
func fullReindexEmbedding() {
	if !isEmbeddingEnabled() {
		logging.LogWarnf("embedding not enabled, skip reindex")
		return
	}
	if !checkEmbeddingTable() {
		logging.LogWarnf("block_embeddings table not available, skip reindex")
		return
	}
	if err := sql.Exec("DELETE FROM block_embeddings"); err != nil {
		logging.LogErrorf("clear block_embeddings failed: %s", err)
		return
	}
	logging.LogInfof("embedding vectors cleared, indexer will re-embed all blocks")

	// If the background indexer's infinite loop isn't running (e.g. embedding was disabled when the kernel started
	// and the user enabled it and clicked rebuild afterward), start it here.
	// StartEmbeddingIndexer internally uses CAS to guarantee only one infinite loop is started. If it's already
	// running, publish an event to wake it up and catch up immediately, instead of waiting for the 30s fallback poll.
	if !embeddingIndexerRunning.Load() {
		go StartEmbeddingIndexer()
	} else {
		eventbus.Publish(eventbus.EvtEmbeddingDirty, "")
	}
}

// RetryFailedEmbedding deletes the rows for all failed blocks so they immediately return to the main loop to be
// re-embedded. It runs asynchronously: it returns immediately after queueing the task.
// The difference from ReindexEmbedding: this only deletes failed blocks with fail_count>0 (embedding is empty, no
// valid vector); vectors that already succeeded are left untouched.
func RetryFailedEmbedding() {
	task.AppendTask(task.DatabaseIndexEmbeddingRetryFailed, retryFailedEmbedding)
}

// retryFailedEmbedding is the actual retry logic, scheduled and executed by the task queue.
// A failed block's embedding is empty (written as []byte{} on failure), so deleting it loses no valid vector; once
// the row is deleted, the block once again satisfies e.id IS NULL in the pending query.
func retryFailedEmbedding() {
	if !isEmbeddingEnabled() {
		logging.LogWarnf("embedding not enabled, skip retry failed")
		return
	}
	if !checkEmbeddingTable() {
		logging.LogWarnf("block_embeddings table not available, skip retry failed")
		return
	}
	if err := sql.Exec("DELETE FROM block_embeddings WHERE fail_count > 0"); err != nil {
		logging.LogErrorf("delete failed embedding rows failed: %s", err)
		return
	}
	logging.LogInfof("failed embedding rows cleared, indexer will retry these blocks")
	// Wake up the resident indexer to catch up immediately
	if embeddingIndexerRunning.Load() {
		eventbus.Publish(eventbus.EvtEmbeddingDirty, "")
	} else {
		go StartEmbeddingIndexer()
	}
}

// EmbeddingStat holds embedding indexing progress statistics, for display on the settings page.
type EmbeddingStat struct {
	Total           int  `json:"total"`           // Total block count in the blocks table (the denominator)
	Indexed         int  `json:"indexed"`         // Number of valid vectors (length(embedding)>0)
	Pending         int  `json:"pending"`         // Number of blocks pending indexing (blocks with no corresponding block_embeddings row)
	Failed          int  `json:"failed"`          // Number of failed blocks (fail_count>0)
	IgnoredByLen    int  `json:"ignoredByLen"`    // Ignored by length (content too short or too long, ignored_type=1)
	IgnoredByConfig int  `json:"ignoredByConfig"` // Ignored by config (matched by .siyuan/embeddingignore, ignored_type=2)
	Enabled         bool `json:"enabled"`         // Whether embedding is enabled
}

// GetEmbeddingStat queries embedding indexing progress statistics. Returns zero-value stats if the table doesn't
// exist or embedding isn't enabled.
func GetEmbeddingStat() (ret *EmbeddingStat) {
	ret = &EmbeddingStat{Enabled: isEmbeddingEnabled()}
	if !checkEmbeddingTable() {
		return
	}

	// One SQL query computes both total and pending (pending = blocks with no corresponding embedding row)
	// COALESCE handles the NULL from the LEFT JOIN; use the comma-ok type assertion to avoid a panic from driver
	// return type differences
	rows, err := sql.QueryNoLimit("SELECT COUNT(*) AS total, SUM(CASE WHEN e.id IS NULL THEN 1 ELSE 0 END) AS pending FROM blocks b LEFT JOIN block_embeddings e ON b.id = e.id")
	if err != nil || 1 > len(rows) {
		logging.LogErrorf("query embedding total/pending stat failed: %s", err)
		return
	}
	if total, ok := rows[0]["total"].(int64); ok {
		ret.Total = int(total)
	}
	if pending, ok := rows[0]["pending"].(int64); ok {
		ret.Pending = int(pending)
	}

	// Indexed (valid vectors)
	rows, err = sql.QueryNoLimit("SELECT COUNT(*) AS c FROM block_embeddings WHERE length(embedding) > 0")
	if err == nil && 0 < len(rows) {
		if c, ok := rows[0]["c"].(int64); ok {
			ret.Indexed = int(c)
		}
	}

	// Failed blocks (includes both those currently retrying and permanent failures, counted together so the user is aware)
	rows, err = sql.QueryNoLimit("SELECT COUNT(*) AS c FROM block_embeddings WHERE fail_count > 0")
	if err == nil && 0 < len(rows) {
		if c, ok := rows[0]["c"].(int64); ok {
			ret.Failed = int(c)
		}
	}

	// Count ignored blocks separately by reason: ignored_type=1 is ignored by length, =2 is ignored by config
	rows, err = sql.QueryNoLimit("SELECT SUM(CASE WHEN ignored_type = 1 THEN 1 ELSE 0 END) AS by_len, SUM(CASE WHEN ignored_type = 2 THEN 1 ELSE 0 END) AS by_conf FROM block_embeddings WHERE ignored_type > 0")
	if err == nil && 0 < len(rows) {
		if byLen, ok := rows[0]["by_len"].(int64); ok {
			ret.IgnoredByLen = int(byLen)
		}
		if byConf, ok := rows[0]["by_conf"].(int64); ok {
			ret.IgnoredByConfig = int(byConf)
		}
	}
	return
}

func embeddingKey() string {
	if nil != Conf.AI.Embedding && Conf.AI.Embedding.Enabled && "" != Conf.AI.Embedding.APIKey {
		return Conf.AI.Embedding.APIKey
	}
	if v := os.Getenv("SIYUAN_OPENAI_EMBEDDING_API_KEY"); "" != v {
		return v
	}
	return ""
}

func embeddingBaseURL() string {
	if nil != Conf.AI.Embedding && Conf.AI.Embedding.Enabled && "" != Conf.AI.Embedding.BaseURL {
		return Conf.AI.Embedding.BaseURL
	}
	if v := os.Getenv("SIYUAN_OPENAI_EMBEDDING_BASE_URL"); "" != v {
		return v
	}
	return ""
}

func embeddingTimeout() int {
	if nil != Conf.AI.Embedding && Conf.AI.Embedding.Enabled && 0 < Conf.AI.Embedding.Timeout {
		return Conf.AI.Embedding.Timeout
	}
	return 30
}

// embeddingDimensions returns the configured output vector dimension. 0 means use the model's default dimension (the
// dimensions parameter is not passed to the API).
// Only text-embedding-3 and later models support custom dimensions. Document vectors and query vectors must use the
// same dimension, otherwise the similarity computation will have a dimension mismatch.
func embeddingDimensions() int {
	if nil != Conf.AI.Embedding && Conf.AI.Embedding.Enabled && 0 < Conf.AI.Embedding.Dimensions {
		return Conf.AI.Embedding.Dimensions
	}
	return 0
}

func embeddingModel() string {
	if nil != Conf.AI.Embedding && Conf.AI.Embedding.Enabled && "" != Conf.AI.Embedding.Name {
		return Conf.AI.Embedding.Name
	}
	if v := os.Getenv("SIYUAN_OPENAI_EMBEDDING_MODEL"); "" != v {
		return v
	}
	return ""
}
