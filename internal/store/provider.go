package store

import "sort"

// Provider is one agent's view of session storage: how to find sessions,
// parse them, resume/create them, and trash them. Each provider owns its
// own base directory.
type Provider interface {
	Agent() Agent
	Available() bool
	Scan() ([]Session, error)
	ParseMetadata(path string) (Meta, error)
	ParseTranscript(path string) (Transcript, error)
	Trash(s Session) (string, error)
	ResumeCommand(s Session) (name string, args []string)
	NewCommand() (name string, args []string)
	Binary() string
}

// ProviderFor returns the provider handling agent a, or nil.
func ProviderFor(providers []Provider, a Agent) Provider {
	for _, p := range providers {
		if p.Agent() == a {
			return p
		}
	}
	return nil
}

// ScanAll runs every available provider's Scan and returns the merged
// sessions sorted by LastActivity descending.
func ScanAll(providers []Provider) ([]Session, error) {
	var all []Session
	for _, p := range providers {
		if !p.Available() {
			continue
		}
		ss, err := p.Scan()
		if err != nil {
			return nil, err
		}
		all = append(all, ss...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].LastActivity.After(all[j].LastActivity)
	})
	return all, nil
}
