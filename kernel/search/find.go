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

package search

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

type Match struct {
	Path   string
	Target string
}

func FindAllMatchedPaths(root string, targets []string) []string {
	matches := FindAllMatches(root, targets)
	return pathsFromMatches(matches)
}

func FindAllMatchedTargets(root string, targets []string) []string {
	matches := FindAllMatches(root, targets)
	return targetsFromMatches(matches)
}

// FindAllMatches walks the files under root and returns all matching results (file path + matched target).
// Returns nil when targets is empty or root is empty.
func FindAllMatches(root string, targets []string) []Match {
	if root == "" || len(targets) == 0 {
		return nil
	}

	// Build a pattern index keyed by the first byte, and compute the longest pattern length
	patternIndex := make(map[byte][][]byte)
	var maxLen int
	for _, t := range targets {
		if t == "" {
			continue
		}
		b := []byte(t)
		if len(b) > maxLen {
			maxLen = len(b)
		}
		patternIndex[b[0]] = append(patternIndex[b[0]], b)
	}
	if len(patternIndex) == 0 {
		return nil
	}

	jobs := make(chan string, 256)
	results := make(chan Match, 256)

	var wg sync.WaitGroup
	var collectWg sync.WaitGroup

	var matches []Match
	collectWg.Go(func() {
		for m := range results {
			matches = append(matches, m)
		}
	})

	numWorkers := runtime.NumCPU()
	for range numWorkers {
		wg.Go(func() {
			for p := range jobs {
				hits := scanFileForTargets(p, patternIndex, maxLen)
				if len(hits) > 0 {
					for _, t := range hits {
						results <- Match{Path: p, Target: t}
					}
				}
			}
		})
	}

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			jobs <- path
		}
		return nil
	})

	close(jobs)
	wg.Wait()
	close(results)
	collectWg.Wait()
	return matches
}

// scanFileForTargets streams through a file searching for all targets (using the first-byte index), and returns a de-duplicated list of matched target strings
func scanFileForTargets(path string, patternIndex map[byte][][]byte, maxLen int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Build a byte bitmap to speed up first-byte detection
	var bitmap [256]bool
	for b := range patternIndex {
		bitmap[b] = true
	}

	found := make(map[string]struct{})
	buf := make([]byte, 64<<10) // 64KB
	// Keep the overlap data from the end of the previous chunk, to support matches that span chunk boundaries
	var tail []byte

	for {
		n, err := f.Read(buf)
		if n > 0 {
			// data = tail + buf[:n]
			data := make([]byte, len(tail)+n)
			copy(data, tail)
			copy(data[len(tail):], buf[:n])

			// Scan data, looking for any candidate first-byte position
			i := 0
			for i < len(data) {
				// Quickly skip non-candidate bytes
				for i < len(data) && !bitmap[data[i]] {
					i++
				}
				if i >= len(data) {
					break
				}
				b := data[i]
				// Verify all patterns for the matching first byte
				for _, pat := range patternIndex[b] {
					pl := len(pat)
					// If the remaining bytes are insufficient for a full match, defer it to the next round (guaranteed via tail)
					if i+pl <= len(data) {
						if bytes.Equal(pat, data[i:i+pl]) {
							found[string(pat)] = struct{}{}
						}
					}
				}
				i++
			}

			// Keep the last maxLen-1 bytes as the tail for the next chunk (avoids an oversized allocation)
			if maxLen <= 1 {
				tail = nil
			} else {
				if len(data) >= maxLen-1 {
					tail = append(tail[:0], data[len(data)-(maxLen-1):]...)
				} else {
					tail = append(tail[:0], data...)
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			// Read error, return the results collected so far
			break
		}
	}

	if len(found) == 0 {
		return nil
	}
	res := make([]string, 0, len(found))
	for k := range found {
		res = append(res, k)
	}
	return res
}

// pathsFromMatches returns a de-duplicated slice of paths from a Match list (preserving first-occurrence order)
func pathsFromMatches(ms []Match) []string {
	if len(ms) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	paths := make([]string, 0)
	for _, m := range ms {
		if _, ok := seen[m.Path]; ok {
			continue
		}
		seen[m.Path] = struct{}{}
		paths = append(paths, m.Path)
	}
	return paths
}

// targetsFromMatches returns a de-duplicated slice of targets from a Match list (preserving first-occurrence order)
func targetsFromMatches(ms []Match) []string {
	if len(ms) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	targets := make([]string, 0)
	for _, m := range ms {
		if _, ok := seen[m.Target]; ok {
			continue
		}
		seen[m.Target] = struct{}{}
		targets = append(targets, m.Target)
	}
	return targets
}
