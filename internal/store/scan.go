package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Scan lists every session file under projectsDir, most recent first.
// It reads only directory entries — no file contents — so it is fast
// enough to run synchronously before first paint.
func Scan(projectsDir string) ([]Session, error) {
	dirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, err
	}
	var sessions []Session
	for _, d := range dirs {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		files, err := os.ReadDir(filepath.Join(projectsDir, d.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			s := Session{
				ID:   strings.TrimSuffix(f.Name(), ".jsonl"),
				Path: filepath.Join(projectsDir, d.Name(), f.Name()),
				Slug: d.Name(),
			}
			if info, err := f.Info(); err == nil {
				s.LastActivity = info.ModTime()
			}
			sessions = append(sessions, s)
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActivity.After(sessions[j].LastActivity)
	})
	return sessions, nil
}

type EnrichResult struct {
	Index int
	Meta  Meta
	Err   error
}

// Enrich parses metadata for every session concurrently, dispatching to the
// provider that handles each session's Agent, and sends one result per
// session (closing results when done). Sessions whose agent has no provider
// yield an error result.
func Enrich(sessions []Session, providers []Provider, workers int, results chan<- EnrichResult) {
	if workers < 1 {
		workers = 1
	}
	type job struct {
		path  string
		slug  string
		agent Agent
	}
	snap := make([]job, len(sessions))
	for i, s := range sessions {
		snap[i] = job{s.Path, s.Slug, s.Agent}
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				p := ProviderFor(providers, snap[i].agent)
				if p == nil {
					results <- EnrichResult{Index: i, Err: fmt.Errorf("no provider for agent %q", snap[i].agent)}
					continue
				}
				m, err := p.ParseMetadata(snap[i].path)
				if err == nil && m.CWD == "" && snap[i].agent == AgentClaude {
					m.CWD = ResolveSlug("/", snap[i].slug)
				}
				results <- EnrichResult{Index: i, Meta: m, Err: err}
			}
		}()
	}
	go func() {
		for i := range snap {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
}

// ResolveSlug maps a projects-dir slug like "-home-william-hyper-sagnn"
// back to a filesystem path. Slugs replace "/" with "-", which collides
// with dashes inside directory names, so it tries every split and
// returns the longest candidate that exists under root ("" if none).
func ResolveSlug(root, slug string) string {
	tokens := strings.Split(strings.TrimPrefix(slug, "-"), "-")
	best := ""
	var walk func(prefix string, i int)
	walk = func(prefix string, i int) {
		if i == len(tokens) {
			full := filepath.Join(root, prefix)
			if st, err := os.Stat(full); err == nil && st.IsDir() && len(full) > len(best) {
				best = full
			}
			return
		}
		walk(prefix+"/"+tokens[i], i+1)
		if i > 0 {
			walk(prefix+"-"+tokens[i], i+1)
		}
	}
	walk("", 0)
	return best
}

// KnownDirs returns the unique, still-existing working directories of
// sessions, preserving input order (callers pass recency-sorted slices).
func KnownDirs(sessions []Session) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, s := range sessions {
		if s.CWD == "" || seen[s.CWD] {
			continue
		}
		seen[s.CWD] = true
		if st, err := os.Stat(s.CWD); err == nil && st.IsDir() {
			dirs = append(dirs, s.CWD)
		}
	}
	return dirs
}
