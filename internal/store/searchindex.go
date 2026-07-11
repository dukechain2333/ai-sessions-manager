package store

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// indexMsgSep joins extracted messages in a cache file. \x1e (record
// separator) cannot occur in message text, so splitting is unambiguous.
const indexMsgSep = "\n\x1e\n"

// SearchIndex is a per-session plain-text cache of message content, used
// by the full-text search layer. One file per session under Dir; line 1 is
// the validity key "path\tmtimeUnixNano\tsize", the body is the messages
// joined by indexMsgSep. Tool messages are excluded at extraction time.
// Session paths must not contain newlines — line 1 of a cache file is the header.
type SearchIndex struct {
	Dir string
}

// NewSearchIndex places the cache under the platform user-cache dir.
func NewSearchIndex() (SearchIndex, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return SearchIndex{}, err
	}
	dir := filepath.Join(base, "sm-index")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SearchIndex{}, err
	}
	return SearchIndex{Dir: dir}, nil
}

func (ix SearchIndex) cacheFile(sessionPath string) string {
	sum := sha1.Sum([]byte(sessionPath))
	return filepath.Join(ix.Dir, hex.EncodeToString(sum[:])+".txt")
}

func validityKey(sessionPath string) (string, error) {
	st, err := os.Stat(sessionPath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\t%d\t%d", sessionPath, st.ModTime().UnixNano(), st.Size()), nil
}

// EnsureSession makes the cache file for one session fresh: a no-op when
// the validity key matches, otherwise a streaming re-extract written
// atomically (temp file + rename) so a failed extraction never leaves a
// half-indexed session behind.
func (ix SearchIndex) EnsureSession(sessionPath string) error {
	key, err := validityKey(sessionPath)
	if err != nil {
		return err
	}
	if cur, ok := ix.readKey(sessionPath); ok && cur == key {
		return nil
	}
	tr, err := ParseTranscript(sessionPath)
	if err != nil {
		return err
	}
	// ParseTranscript never emits empty user/assistant text, so a
	// zero-message body is unambiguous (join of one empty string would
	// collide with it).
	var texts []string
	for _, m := range tr.Messages {
		if m.Kind == KindTool {
			continue
		}
		texts = append(texts, m.Text)
	}
	tmp, err := os.CreateTemp(ix.Dir, "tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(key + "\n" + strings.Join(texts, indexMsgSep))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmp.Name(), ix.cacheFile(sessionPath)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// readKey returns the cache file's header line. Freshness is decided by
// the callers' direct comparison against the recomputed validity key —
// any corruption simply fails that comparison and triggers a rebuild.
func (ix SearchIndex) readKey(sessionPath string) (string, bool) {
	data, err := os.ReadFile(ix.cacheFile(sessionPath))
	if err != nil {
		return "", false
	}
	head, _, _ := strings.Cut(string(data), "\n")
	return head, true
}

// Messages returns the cached message texts for a session and whether the
// cache is fresh (validity key matches the live file). Stale, corrupt, or
// missing caches — and missing sources — return (nil, false).
func (ix SearchIndex) Messages(sessionPath string) ([]string, bool) {
	key, err := validityKey(sessionPath)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(ix.cacheFile(sessionPath))
	if err != nil {
		return nil, false
	}
	head, body, _ := strings.Cut(string(data), "\n")
	if head != key {
		return nil, false
	}
	if body == "" {
		return []string{}, true
	}
	return strings.Split(body, indexMsgSep), true
}

// IndexProgress is one EnsureAll progress tick: Done sessions out of Total.
type IndexProgress struct {
	Done, Total int
	Err         error
}

// EnsureAll freshens the cache for every session concurrently, sending one
// IndexProgress per session and closing results when done — the same
// shape as Enrich, so the UI can reuse its channel-pump pattern.
func (ix SearchIndex) EnsureAll(sessions []Session, workers int, results chan<- IndexProgress) {
	if workers < 1 {
		workers = 1
	}
	paths := make([]string, len(sessions))
	for i, s := range sessions {
		paths[i] = s.Path
	}
	jobs := make(chan int)
	done := make(chan error)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				done <- ix.EnsureSession(paths[i])
			}
		}()
	}
	go func() {
		for i := range paths {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(done)
	}()
	go func() {
		n := 0
		for err := range done {
			n++
			results <- IndexProgress{Done: n, Total: len(paths), Err: err}
		}
		close(results)
	}()
}

// SessionHits is one full-text search result: the session's index in the
// slice handed to Search, how many messages hit, and the first hit message.
type SessionHits struct {
	Session int
	MsgHits int
	First   int
}

// SplitTerms lower-cases and splits a query on whitespace.
func SplitTerms(q string) []string {
	return strings.Fields(strings.ToLower(q))
}

// Search runs a case-insensitive AND-of-terms search over the cached
// message text of sessions. Sessions without a fresh cache are skipped
// (indexed reports how many were searchable). Hits are ordered by message
// hit count desc, then recency desc, then slice order.
func (ix SearchIndex) Search(query string, sessions []Session) (hits []SessionHits, indexed int) {
	terms := SplitTerms(query)
	if len(terms) == 0 {
		return nil, 0
	}
	// Non-nil even with zero hits: callers use a nil result slice as the
	// "not searching" sentinel, so a real 0-match search must stay distinct
	// from "no search active" (otherwise the list falls back to showing every
	// session while the header claims 0 matched).
	hits = []SessionHits{}
	for si, s := range sessions {
		msgs, fresh := ix.Messages(s.Path)
		if !fresh {
			continue
		}
		indexed++
		h := SessionHits{Session: si, First: -1}
		remaining := make(map[string]bool, len(terms))
		for _, t := range terms {
			remaining[t] = true
		}
		for mi, m := range msgs {
			lower := strings.ToLower(m)
			hit := false
			for _, t := range terms {
				if strings.Contains(lower, t) {
					hit = true
					delete(remaining, t)
				}
			}
			if hit {
				h.MsgHits++
				if h.First < 0 {
					h.First = mi
				}
			}
		}
		if h.MsgHits > 0 && len(remaining) == 0 { // every term appeared somewhere
			hits = append(hits, h)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].MsgHits != hits[j].MsgHits {
			return hits[i].MsgHits > hits[j].MsgHits
		}
		return sessions[hits[i].Session].LastActivity.After(sessions[hits[j].Session].LastActivity)
	})
	return hits, indexed
}
