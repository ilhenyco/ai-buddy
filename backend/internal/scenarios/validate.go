package scenarios

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ilhenyco/ai-buddy/backend/internal/platform/httpx"
)

// Limits are proposals (openapi.yaml: "лимиты — предложения для проверки").
const (
	maxBlocks    = 40
	maxTasks     = 20
	maxTitle     = 200
	maxText      = 4000
	maxNameRunes = 200
)

var (
	idPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,119}$`) // frontend uses blockId+"-"+UUID
	stageKinds = set("start", "week", "adaptation", "practice", "midpoint", "final-prep", "final", "after", "generic")
	taskKinds  = set("team", "buddy", "lead", "hr", "access", "task", "courses", "practice", "midpoint", "final", "handoff")
)

func set(v ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range v {
		m[s] = true
	}
	return m
}

func validDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// ValidExternalURL: only http/https with a host (validExternalUrl in the frontend).
func ValidExternalURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func tooLong(s string, n int) bool { return utf8.RuneCountInString(s) > n }

// ValidateStructure checks a draft: empty texts and dates are allowed,
// but the structure, ids, enums, dates that are filled in and URLs must be correct.
func ValidateStructure(p Plan) error {
	var bad []string
	add := func(f string, args ...any) { bad = append(bad, fmt.Sprintf(f, args...)) }

	if p.Months != 6 && p.Months != 9 {
		add("months")
	}
	if p.Start != "" && !validDate(p.Start) {
		add("start")
	}
	if tooLong(p.Name, maxNameRunes) {
		add("name")
	}
	if len(p.Blocks) == 0 || len(p.Blocks) > maxBlocks {
		add("blocks")
	}
	blockIDs := map[string]bool{}
	for i, b := range p.Blocks {
		if !idPattern.MatchString(b.ID) || blockIDs[b.ID] {
			add("blocks[%d].id", i)
		}
		blockIDs[b.ID] = true
		if b.StageKind != "" && !stageKinds[b.StageKind] {
			add("blocks[%d].stageKind", i)
		}
		if b.Date != "" && !validDate(b.Date) {
			add("blocks[%d].date", i)
		}
		if b.EndDate != "" && !validDate(b.EndDate) {
			add("blocks[%d].endDate", i)
		}
		if tooLong(b.Title, maxTitle) {
			add("blocks[%d].title", i)
		}
		for name, text := range map[string]string{"intern": b.Intern, "buddy": b.Buddy, "lead": b.Lead} {
			if tooLong(text, maxText) {
				add("blocks[%d].%s", i, name)
			}
		}
		if len(b.Tasks) > maxTasks {
			add("blocks[%d].tasks", i)
		}
		taskIDs := map[string]bool{}
		for j, t := range b.Tasks {
			if !idPattern.MatchString(t.ID) || taskIDs[t.ID] {
				add("blocks[%d].tasks[%d].id", i, j)
			}
			taskIDs[t.ID] = true
			if !taskKinds[t.Kind] {
				add("blocks[%d].tasks[%d].kind", i, j)
			}
			if t.URL != "" && !ValidExternalURL(t.URL) {
				add("blocks[%d].tasks[%d].url", i, j)
			}
			if tooLong(t.Title, maxTitle) || tooLong(t.Link, maxTitle) {
				add("blocks[%d].tasks[%d].title", i, j)
			}
			if tooLong(t.Description, maxText) {
				add("blocks[%d].tasks[%d].description", i, j)
			}
			if t.Actions != nil && (tooLong(t.Actions.Buddy, maxText) || tooLong(t.Actions.Lead, maxText)) {
				add("blocks[%d].tasks[%d].actions", i, j)
			}
		}
	}
	if len(bad) > 0 {
		return httpx.Validation("Проверьте структуру черновика.", bad...)
	}
	return nil
}

// ValidateForPublish repeats publicationError from frontend/src/scenarios.ts:
// a draft may be incomplete, a publication may not.
func ValidateForPublish(p Plan) error {
	if err := ValidateStructure(p); err != nil {
		return err
	}
	var bad []string
	blank := func(s string) bool { return strings.TrimSpace(s) == "" }
	if blank(p.Name) {
		bad = append(bad, "name")
	}
	if p.InternID == nil || p.BuddyID == nil || p.LeadID == nil {
		bad = append(bad, "internId")
	}
	if p.Start == "" {
		bad = append(bad, "start")
	}
	for i, b := range p.Blocks {
		if blank(b.Title) {
			bad = append(bad, fmt.Sprintf("blocks[%d].title", i))
		}
		if b.Date == "" {
			bad = append(bad, fmt.Sprintf("blocks[%d].date", i))
		}
		if blank(b.Intern) {
			bad = append(bad, fmt.Sprintf("blocks[%d].intern", i))
		}
		for j, t := range b.Tasks {
			if blank(t.Title) {
				bad = append(bad, fmt.Sprintf("blocks[%d].tasks[%d].title", i, j))
			}
		}
	}
	if len(bad) > 0 {
		return httpx.Validation("Заполните обязательные поля перед публикацией.", bad...)
	}
	return nil
}
