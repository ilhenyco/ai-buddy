package features_test

import (
	"reflect"
	"testing"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/features"
)

func TestParse(t *testing.T) {
	all := []string{"access_integration", "ai_chat", "events_publisher", "mcp_server"}
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"", all},
		{"all", all},
		{" none ", []string{}},
		{"ai_chat, events_publisher", []string{"ai_chat", "events_publisher"}},
		{"all,-ai_chat", []string{"access_integration", "events_publisher", "mcp_server"}},
		{"none,ai_chat,", []string{"ai_chat"}},
	} {
		s, err := features.Parse(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got := s.List(); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%q: %v, want %v", c.in, got, c.want)
		}
	}
	if _, err := features.Parse("ai-chat"); err == nil {
		t.Fatal("a typo must be an error")
	}
	var none features.Set
	if !none.On(features.AIChat) || len(none.List()) != 4 {
		t.Fatal("a nil set has every flag on")
	}
}
