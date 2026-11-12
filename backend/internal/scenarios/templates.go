package scenarios

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"time"
)

// templates.json is exported from frontend/src/domain.ts (makePlan(6), makePlan(9)),
// so the server and the prototype start from identical blocks.
//
//go:embed templates.json
var templatesJSON []byte

// PoolVersion changes whenever templates.json changes.
const PoolVersion = "2026-11-12"

type templateFile struct {
	ID     string  `json:"id"`
	Months int     `json:"months"`
	Start  string  `json:"start"`
	Name   string  `json:"name"`
	Blocks []Block `json:"blocks"`
}

var builtin = mustLoad()

func mustLoad() []templateFile {
	var t []templateFile
	if err := json.Unmarshal(templatesJSON, &t); err != nil {
		panic(fmt.Sprintf("templates.json: %v", err))
	}
	return t
}

// Templates returns the built-in templates (dates relative to their base start).
func Templates() []Template {
	out := make([]Template, 0, len(builtin))
	for _, t := range builtin {
		out = append(out, Template{ID: t.ID, Months: t.Months, Blocks: cloneBlocks(t.Blocks), PoolVersion: PoolVersion})
	}
	return out
}

// FromTemplate builds a new draft plan: template blocks shifted to start
// (same as shiftPlanStart in the frontend).
func FromTemplate(months int, start string) (Plan, error) {
	for _, t := range builtin {
		if t.Months != months {
			continue
		}
		days, err := daysBetween(t.Start, start)
		if err != nil {
			return Plan{}, err
		}
		blocks := cloneBlocks(t.Blocks)
		for i := range blocks {
			blocks[i].Date = shiftDate(blocks[i].Date, days)
			blocks[i].EndDate = shiftDate(blocks[i].EndDate, days)
		}
		return Plan{Name: t.Name, Start: start, Months: months, Blocks: blocks}, nil
	}
	return Plan{}, fmt.Errorf("no template for %d months", months)
}

func daysBetween(from, to string) (int, error) {
	a, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return 0, err
	}
	b, err := time.Parse(time.DateOnly, to)
	if err != nil {
		return 0, err
	}
	return int(b.Sub(a).Hours() / 24), nil
}

func shiftDate(d string, days int) string {
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return d
	}
	return t.AddDate(0, 0, days).Format(time.DateOnly)
}

func cloneBlocks(in []Block) []Block {
	b, _ := json.Marshal(in)
	var out []Block
	_ = json.Unmarshal(b, &out)
	return out
}
