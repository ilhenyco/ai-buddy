// Package messages: Хомыч writes by the calendar. When a block of the active plan is due,
// the scheduler creates one event and a message for each participant (intern, buddy, lead)
// with that role's text and checklist. Contract: listMessages, setCheck, listNotifications.
// Rules: docs/ai-buddy/server-handoff.md ("Обязательные инварианты данных").
package messages

import (
	"time"

	"github.com/ilhenyco/ai-buddy/backend/internal/scenarios"
)

// NextBlock is the "Дальше — …" line under a message.
// Extension of the contract (Message.next), see backend/README.md.
type NextBlock struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Date  string `json:"date"`
}

type Message struct {
	ID              string           `json:"id"`
	AssignmentID    string           `json:"assignmentId"`
	Kind            string           `json:"kind"`
	RecipientUserID string           `json:"recipientUserId"`
	CreatedAt       time.Time        `json:"createdAt"`
	Text            string           `json:"text"`
	ScenarioID      string           `json:"scenarioId,omitempty"`
	RevisionID      string           `json:"revisionId,omitempty"`
	Block           *scenarios.Block `json:"block,omitempty"`
	Next            *NextBlock       `json:"next,omitempty"`
}

type Check struct {
	MessageID string    `json:"messageId"`
	TaskID    string    `json:"taskId"`
	UserID    string    `json:"userId"`
	Checked   bool      `json:"checked"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type SetCheck struct {
	Checked *bool `json:"checked"`
}

// ActivePlanInfo tells the client that a plan is published even before the first block is due.
// Extension of the contract (FeedPage.activePlan), see backend/README.md.
type ActivePlanInfo struct {
	ScenarioID string `json:"scenarioId"`
	RevisionID string `json:"revisionId"`
	Start      string `json:"start"`
	Months     int    `json:"months"`
}

type FeedPage struct {
	Items      []Message       `json:"items"`
	NextCursor *string         `json:"nextCursor"`
	Checks     []Check         `json:"checks"`
	ServerTime time.Time       `json:"serverTime"`
	ActivePlan *ActivePlanInfo `json:"activePlan"`
}

type Notification struct {
	ID             string    `json:"id"`
	MessageID      string    `json:"messageId"`
	AssignmentID   string    `json:"assignmentId"`
	Title          string    `json:"title"`
	CreatedAt      time.Time `json:"createdAt"`
	DeliveryStatus string    `json:"deliveryStatus"`
}

type NotificationPage struct {
	Items      []Notification `json:"items"`
	NextCursor *string        `json:"nextCursor"`
}

// ClockState is the answer of the demo clock endpoints.
type ClockState struct {
	Today   string `json:"today"`
	Shifted bool   `json:"shifted"`
	Created int    `json:"created"`
}
