package store

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// indexMsgSep joins extracted messages in a cache file. \x1e (record
// separator) cannot occur in message text, so splitting is unambiguous.
const indexMsgSep = "\n\x1e\n"

// SearchIndex is a per-session plain-text cache of message content, used
// by the full-text search layer. One file per session under Dir; line 1 is
// the validity key "path\tmtimeUnixNano\tsize", the body is the messages
// joined by indexMsgSep. Tool messages are excluded at extraction time.
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
	return os.Rename(tmp.Name(), ix.cacheFile(sessionPath))
}

func (ix SearchIndex) readKey(sessionPath string) (string, bool) {
	data, err := os.ReadFile(ix.cacheFile(sessionPath))
	if err != nil {
		return "", false
	}
	head, _, _ := strings.Cut(string(data), "\n")
	if strings.Count(head, "\t") != 2 {
		return "", false // corrupt header
	}
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
