// Package features holds the feature flags of the technical specification: one switch per
// integration, so a stand can run without AI, without the access service, without the MCP
// server or without publishing business events. FEATURES lists what is on (default "all").
package features

import (
	"fmt"
	"sort"
	"strings"
)

const (
	AIChat            = "ai_chat"            // questions to Хомыч, answers and stage suggestions
	AccessIntegration = "access_integration" // access requests to the external access service
	MCPServer         = "mcp_server"         // MCP endpoint of the knowledge service (read by Python)
	EventsPublisher   = "events_publisher"   // business events through the outbox
)

// All is every known flag, in a stable order.
var All = []string{AIChat, AccessIntegration, MCPServer, EventsPublisher}

// Set is the enabled flags. A nil Set means everything is on.
type Set map[string]bool

// Parse reads FEATURES: "all" (or empty) — every flag, "none" — none, otherwise a comma list of
// enabled flags. "all,-ai_chat" switches one flag off. An unknown name is an error: a typo must
// not silently switch an integration off.
func Parse(s string) (Set, error) {
	out := Set{}
	s = strings.TrimSpace(s)
	if s == "" {
		s = "all"
	}
	for _, part := range strings.Split(s, ",") {
		name := strings.TrimSpace(part)
		off := strings.HasPrefix(name, "-")
		name = strings.TrimPrefix(name, "-")
		switch {
		case name == "":
			continue
		case name == "all":
			for _, f := range All {
				out[f] = !off
			}
		case name == "none":
			for _, f := range All {
				out[f] = false
			}
		case known(name):
			out[name] = !off
		default:
			return nil, fmt.Errorf("unknown feature %q (known: %s)", name, strings.Join(All, ", "))
		}
	}
	return out, nil
}

func known(name string) bool {
	for _, f := range All {
		if f == name {
			return true
		}
	}
	return false
}

// On reports whether a flag is enabled. Everything is on in a nil Set.
func (s Set) On(name string) bool {
	if s == nil {
		return true
	}
	return s[name]
}

// List returns the enabled flags, sorted (the "features" of sessionContext).
func (s Set) List() []string {
	out := []string{}
	for _, f := range All {
		if s.On(f) {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}
