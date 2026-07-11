package store

import "os"

// claudeProvider serves Claude Code sessions under projectsDir
// (~/.claude/projects). It reuses the package's existing Claude parsers.
type claudeProvider struct{ projectsDir string }

// NewClaudeProvider builds the Claude provider for projectsDir.
func NewClaudeProvider(projectsDir string) Provider {
	return claudeProvider{projectsDir: projectsDir}
}

func (claudeProvider) Agent() Agent   { return AgentClaude }
func (claudeProvider) Binary() string { return "claude" }

func (p claudeProvider) Available() bool {
	info, err := os.Stat(p.projectsDir)
	return err == nil && info.IsDir()
}

func (p claudeProvider) Scan() ([]Session, error) {
	ss, err := Scan(p.projectsDir)
	if err != nil {
		return nil, err
	}
	for i := range ss {
		ss[i].Agent = AgentClaude
	}
	return ss, nil
}

func (claudeProvider) ParseMetadata(path string) (Meta, error) { return ParseMetadata(path) }
func (claudeProvider) ParseTranscript(path string) (Transcript, error) {
	return ParseTranscript(path)
}
func (p claudeProvider) Trash(s Session) (string, error) {
	return TrashSession(p.projectsDir, s)
}
func (claudeProvider) ResumeCommand(s Session) (string, []string) {
	return "claude", []string{"--resume", s.ID}
}
func (claudeProvider) NewCommand() (string, []string) { return "claude", nil }
