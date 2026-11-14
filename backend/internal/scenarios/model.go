// Package scenarios: the lead builds a personal scenario from a 6/9-month template,
// saves incomplete drafts and publishes a variant, which becomes active for the intern.
// Contract: openapi.yaml operations listTemplates, listScenarios, createScenario,
// getScenario, saveDraft, publishScenario. Rules: frontend/src/scenarios.ts and
// docs/architecture/server-handoff.md ("Обязательные инварианты данных").
package scenarios

import "time"

// Types mirror components.schemas in openapi.yaml.

type RoleActions struct {
	Buddy string `json:"buddy,omitempty"`
	Lead  string `json:"lead,omitempty"`
}

type Task struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Kind        string       `json:"kind"`
	Link        string       `json:"link"`
	URL         string       `json:"url,omitempty"`
	Actions     *RoleActions `json:"actions,omitempty"`
}

type Block struct {
	ID        string `json:"id"`
	StageKind string `json:"stageKind,omitempty"`
	Title     string `json:"title"`
	Date      string `json:"date"`
	EndDate   string `json:"endDate,omitempty"`
	Intern    string `json:"intern"`
	Buddy     string `json:"buddy"`
	Lead      string `json:"lead"`
	Tasks     []Task `json:"tasks"`
}

type Plan struct {
	Name     string  `json:"name"`
	InternID *string `json:"internId"`
	BuddyID  *string `json:"buddyId"`
	LeadID   *string `json:"leadId"`
	TeamID   string  `json:"teamId"`
	Start    string  `json:"start"`
	Months   int     `json:"months"`
	Blocks   []Block `json:"blocks"`
}

type Revision struct {
	ID          string    `json:"id"`
	Number      int       `json:"number"`
	PublishedAt time.Time `json:"publishedAt"`
	Plan        Plan      `json:"plan"`
}

type Scenario struct {
	ID                    string    `json:"id"`
	AssignmentID          *string   `json:"assignmentId"`
	Draft                 Plan      `json:"draft"`
	DraftVersion          int       `json:"draftVersion"`
	Published             *Revision `json:"published"`
	IsActive              bool      `json:"isActive"`
	HasUnpublishedChanges bool      `json:"hasUnpublishedChanges"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

type Counts struct {
	All       int `json:"all"`
	Drafts    int `json:"drafts"`
	Published int `json:"published"`
}

type ScenarioPage struct {
	Items      []Scenario `json:"items"`
	NextCursor *string    `json:"nextCursor"`
	Counts     Counts     `json:"counts"`
}

type CreateScenario struct {
	AssignmentID *string `json:"assignmentId"`
	Months       int     `json:"months"`
	Start        string  `json:"start"`
	Name         string  `json:"name"`
}

type SaveDraft struct {
	Plan Plan `json:"plan"`
}

type Publish struct {
	DraftVersion             int     `json:"draftVersion"`
	ExpectedActiveScenarioID *string `json:"expectedActiveScenarioId"`
	ConfirmReplacement       bool    `json:"confirmReplacement"`
}

type Publication struct {
	Scenario           Scenario `json:"scenario"`
	ReplacedScenarioID *string  `json:"replacedScenarioId"`
	CutoffDate         *string  `json:"cutoffDate"`
	ActiveRevisionID   string   `json:"activeRevisionId"`
}

type Template struct {
	ID          string  `json:"id"`
	Months      int     `json:"months"`
	Blocks      []Block `json:"blocks"`
	PoolVersion string  `json:"poolVersion"`
}

type TemplatePage struct {
	Items []Template `json:"items"`
}

// ActivePlan is what the scheduler (module of Бэк-1) reads: the published plan
// that is currently sending messages for an assignment.
type ActivePlan struct {
	AssignmentID   string
	ScenarioID     string
	RevisionID     string
	RevisionNumber int
	Plan           Plan
	// CutoffDate: if set, send only blocks with Date > CutoffDate (no backfill after replacement).
	CutoffDate *string
}
