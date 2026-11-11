package api

import "time"

// BusMember is an agent in a channel, as the window draws it.
type BusMember struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

// BusChannel is a channel or DM as the sidebar lists it.
type BusChannel struct {
	ID      string      `json:"id"`
	Title   string      `json:"title"` // #name, @agent (a DM with you), a ↔ b
	Topic   string      `json:"topic,omitempty"`
	DM      bool        `json:"dm,omitempty"`
	Yours   bool        `json:"yours,omitempty"` // a DM with you: you may write in it
	Members []BusMember `json:"members"`
	// Last is the number of the last message, as it always was; LastText is
	// it as a line of a list ("who: its first line"), LastAt when it was said
	// (RFC 3339).
	Last     int       `json:"last"`
	LastText string    `json:"last_text"`
	LastAt   time.Time `json:"last_at,omitzero"`
	Unread   int       `json:"unread,omitempty"`
	// Mention: an unread message in it asks you; Listed: it is in the
	// short list (bus.Yours).
	Mention bool     `json:"mention,omitempty"`
	Listed  bool     `json:"listed,omitempty"`
	Paused  bool     `json:"paused,omitempty"`
	Lead    string   `json:"lead,omitempty"` // the agent that answers for its work
	Team    *BusTeam `json:"team,omitempty"` // the team it is the channel of
}

// BusTeam is the team a channel belongs to, as the channel's header
// draws it: its roles with their agents, whose live status the window
// has from the agents.
type BusTeam struct {
	Name     string        `json:"name"`
	Template string        `json:"template"`
	Roles    []BusTeamRole `json:"roles"`
}

type BusTeamRole struct {
	Role  string `json:"role"`
	Agent string `json:"agent,omitempty"` // the agent's id
	Lead  bool   `json:"lead,omitempty"`
}

// BusReceipt is how one of the person's messages stands: the agents it
// wakes, each with its state (waiting, delivered, read, replied); none
// when it wakes nobody.
type BusReceipt struct {
	Seq     int             `json:"seq"`
	Targets []BusTargetInfo `json:"targets"`
	Paused  bool            `json:"paused,omitempty"`
}

// BusTargetInfo is an agent a message wakes, by its label.
type BusTargetInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	State string `json:"state"`
}

// BusDMReply answers POST /api/bus/dm: the DM's channel, the message's
// number and the labels of the agents it woke.
type BusDMReply struct {
	Channel string   `json:"channel"`
	Seq     int      `json:"seq"`
	Woke    []string `json:"woke"`
}

// BusReport answers POST /api/bus/report: the board's line posted in the
// channel ("" when there was none to post).
type BusReport struct {
	Line string `json:"line"`
}
