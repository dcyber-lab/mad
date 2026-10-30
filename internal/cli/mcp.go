package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/mcp"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// mcpCmd serves mad's tools to the agent that started it, over stdio
// (`mad mcp --agent <id>`, put on its command line by deck.MCPFlags): the
// bus, to talk with the deck's other agents and the person.
func mcpCmd(args []string, stdio IO) error {
	fs := flags("mcp", stdio.Err)
	id := fs.String("agent", "", "the `id` of the agent it serves")
	if err := fs.Parse(args); err != nil || *id == "" || fs.NArg() > 0 {
		return errUsage
	}
	srv := &mcp.Server{Name: "mad", Version: version(), Instructions: mcpInstructions, Tools: mcpTools(*id), Serial: true}
	return srv.Serve(context.Background(), stdio.In, stdio.Out)
}

const mcpInstructions = `mad's bus: talk with the other agents in this deck, and with the person, in channels and direct messages.
Messages for you arrive as a turn that starts with [mad bus]. Only agents you @mention are interrupted; the rest read when they look.
Post results, questions and decisions; never acknowledgments or progress chatter. If you ask something, end your turn: the answer arrives as a new message.`

func mcpTools(id string) []mcp.Tool {
	obj := func(required []string, props string) json.RawMessage {
		req, _ := json.Marshal(required)
		return json.RawMessage(`{"type":"object","required":` + string(req) + `,"properties":{` + props + `}}`)
	}
	return []mcp.Tool{
		{
			Name:        "bus_who",
			Description: "List the agents in this deck you can message (id, label, kind, status, project, channels) and the channels (members, unread, paused). Call it before addressing someone you have not seen in a message.",
			InputSchema: obj([]string{}, ""),
			Handler:     func(context.Context, json.RawMessage) (string, error) { return mcpWho(id) },
		},
		{
			Name:        "bus_read",
			Description: "Read a channel, or your DM with an agent (give its label), from a sequence number. Use it to catch up before replying, or when a digest said there is more. Reading needs no reply.",
			InputSchema: obj([]string{"channel"}, `"channel":{"type":"string","description":"channel id, or an agent's label for your DM with it"},"since":{"type":"integer","description":"messages after this seq; omit for the last ones"},"limit":{"type":"integer","description":"at most this many (30 by default, 200 at most)"}`),
			Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a struct {
					Channel      string
					Since, Limit int
				}
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				return mcpRead(id, a.Channel, a.Since, a.Limit)
			},
		},
		{
			Name:        "bus_post",
			Description: "Post in a channel. Only agents you @mention (by label, or @all) are interrupted to read it; the rest see it when they look. Post a result, a question or a decision for others; no acknowledgments or progress chatter. If you ask a question, end your turn: the answer arrives as a new message.",
			InputSchema: obj([]string{"channel", "text"}, `"channel":{"type":"string"},"text":{"type":"string","description":"markdown; @label mentions who must read it, @all everyone"},"reply_to":{"type":"integer","description":"the seq of the message you answer"}`),
			Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a struct {
					Channel, Text string
					ReplyTo       int `json:"reply_to"`
				}
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				from, _, err := mcpMe(id)
				if err != nil {
					return "", err
				}
				res, err := bus.Post(a.Channel, from, a.Text, a.ReplyTo)
				if err != nil {
					return "", err
				}
				return mcpJSON(res)
			},
		},
		{
			Name:        "bus_dm",
			Description: "Send one agent, or the person (to: \"you\"), a private message; an agent reads it when its current turn ends. For something several should see, post in a channel instead.",
			InputSchema: obj([]string{"to", "text"}, `"to":{"type":"string","description":"the agent's label or id, or \"you\" for the person"},"text":{"type":"string"}`),
			Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a struct{ To, Text string }
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				from, st, err := mcpMe(id)
				if err != nil {
					return "", err
				}
				// The person, or an agent.
				toID := bus.You
				if t := strings.TrimPrefix(strings.TrimSpace(a.To), "@"); !strings.EqualFold(t, bus.You) {
					to, err := mcpAgent(st, a.To)
					if err != nil {
						return "", err
					}
					if to.ID == id {
						return "", errors.New("that is you")
					}
					toID = to.ID
				}
				ch, res, err := bus.DM(from, toID, a.Text)
				if err != nil {
					return "", err
				}
				return mcpJSON(map[string]any{"channel": ch, "seq": res.Seq, "woke": res.Woke})
			},
		},
		{
			Name:        "bus_channel",
			Description: "Create a channel for a piece of work several agents share, or join, leave or invite to one. Reuse a channel that exists (bus_who lists them). The person is in every channel.",
			InputSchema: obj([]string{"action", "channel"}, `"action":{"type":"string","enum":["create","join","leave","invite"]},"channel":{"type":"string","description":"its name (create) or id"},"topic":{"type":"string"},"members":{"type":"array","items":{"type":"string"},"description":"agents' labels or ids (create, invite)"}`),
			Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a struct {
					Action, Channel, Topic string
					Members                []string
				}
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				return mcpChannel(id, a.Action, a.Channel, a.Topic, a.Members)
			},
		},
	}
}

// mcpMe is the agent the server serves, as the bus names an author.
func mcpMe(id string) (bus.From, *state.State, error) {
	st, err := state.Load()
	if err != nil {
		return bus.From{}, nil, err
	}
	_, a := st.FindAgent(id)
	if a == nil {
		return bus.From{}, nil, errors.New("this agent is no longer in the deck")
	}
	return bus.From{ID: a.ID, Label: drive.Label(a), Kind: a.Kind}, st, nil
}

// mcpAgent finds an agent by id, label (any case) or an id's start.
func mcpAgent(st *state.State, ref string) (*state.Agent, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "@")
	if ref == "" {
		return nil, errors.New("no agent given")
	}
	if _, a := st.FindAgent(ref); a != nil {
		return a, nil
	}
	var found []*state.Agent
	for _, a := range st.OrderedAgents() {
		if strings.EqualFold(drive.Label(a), ref) || len(ref) >= 4 && strings.HasPrefix(a.ID, ref) {
			found = append(found, a)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return nil, fmt.Errorf("no agent %q: bus_who lists them", ref)
	}
	var labels []string
	for _, a := range found {
		labels = append(labels, drive.Label(a)+" ("+drive.ShortID(a.ID)+")")
	}
	return nil, fmt.Errorf("%q is several agents: %s; give an id", ref, strings.Join(labels, ", "))
}

func mcpWho(id string) (string, error) {
	me, st, err := mcpMe(id)
	if err != nil {
		return "", err
	}
	chans, err := bus.Channels()
	if err != nil {
		return "", err
	}
	unread, _ := bus.Unread(id)
	label := map[string]string{bus.You: "you"}
	for _, a := range st.OrderedAgents() {
		label[a.ID] = drive.Label(a)
	}
	in := map[string][]string{}
	type channel struct {
		ID      string   `json:"id"`
		Topic   string   `json:"topic,omitempty"`
		Members []string `json:"members"`
		Unread  int      `json:"unread,omitempty"`
		Paused  bool     `json:"paused,omitempty"`
	}
	var outCh []channel
	for _, c := range chans {
		var members []string
		for _, m := range c.Members {
			in[m] = append(in[m], c.ID)
			if l, ok := label[m]; ok {
				members = append(members, l)
			}
		}
		if strings.HasPrefix(c.ID, "dm.") {
			continue // its own: bus_read with the agent's label
		}
		outCh = append(outCh, channel{c.ID, c.Topic, members, unread[c.ID], c.Paused})
	}
	type agent struct {
		ID       string   `json:"id"`
		Label    string   `json:"label"`
		Kind     string   `json:"kind"`
		Status   string   `json:"status"`
		Project  string   `json:"project"`
		Channels []string `json:"channels,omitempty"`
	}
	var agents []agent
	for _, p := range st.Projects {
		for _, a := range p.Agents {
			if a.ID == id || a.Run != "" {
				continue
			}
			s := "unknown"
			if h := status.ReadHook(a.ID); h != nil && h.State != "" {
				s = h.State
			}
			var chs []string
			for _, c := range in[a.ID] {
				if !strings.HasPrefix(c, "dm.") {
					chs = append(chs, c)
				}
			}
			agents = append(agents, agent{a.ID, drive.Label(a), a.Kind, s, p.Name, chs})
		}
	}
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].Label < agents[j].Label })
	return mcpJSON(map[string]any{"me": map[string]string{"id": me.ID, "label": me.Label}, "agents": agents, "channels": outCh})
}

func mcpRead(id, channel string, since, limit int) (string, error) {
	_, st, err := mcpMe(id)
	if err != nil {
		return "", err
	}
	ch := strings.TrimPrefix(strings.TrimSpace(channel), "#")
	if _, ok, _ := bus.Find(ch); !ok {
		// An agent's label: the DM with it.
		a, err := mcpAgent(st, ch)
		if err != nil {
			return "", fmt.Errorf("no channel %q, nor an agent of that name", channel)
		}
		ch = bus.DMChannel(id, a.ID)
	}
	msgs, last, err := bus.Read(ch, since, limit)
	if err != nil {
		return "", err
	}
	_ = bus.MarkRead(id, ch, last)
	type msg struct {
		Seq     int    `json:"seq"`
		At      string `json:"at"`
		From    string `json:"from"`
		Kind    string `json:"kind,omitempty"`
		Text    string `json:"text"`
		ReplyTo int    `json:"reply_to,omitempty"`
	}
	out := []msg{}
	for _, m := range msgs {
		k := m.Kind
		if k == "text" {
			k = ""
		}
		out = append(out, msg{m.Seq, m.At.Local().Format("15:04"), m.From.Label, k, m.Text, m.ReplyTo})
	}
	c, _, _ := bus.Find(ch)
	return mcpJSON(map[string]any{"channel": ch, "messages": out, "last_seq": last, "paused": c.Paused})
}

func mcpChannel(id, action, channel, topic string, members []string) (string, error) {
	from, st, err := mcpMe(id)
	if err != nil {
		return "", err
	}
	var ids []string
	for _, m := range members {
		a, err := mcpAgent(st, m)
		if err != nil {
			return "", err
		}
		ids = append(ids, a.ID)
	}
	ch := strings.TrimPrefix(strings.TrimSpace(channel), "#")
	switch action {
	case "create":
		c, err := bus.Create(ch, topic, append([]string{id}, ids...), from)
		if err != nil {
			return "", err
		}
		return mcpJSON(map[string]any{"channel": c.ID, "members": len(c.Members)})
	case "join":
		err = bus.Join(ch, id)
	case "leave":
		err = bus.Leave(ch, id)
	case "invite":
		err = bus.Invite(ch, ids)
	default:
		return "", fmt.Errorf("no action %q: create, join, leave or invite", action)
	}
	if err != nil {
		return "", err
	}
	return mcpJSON(map[string]any{"channel": ch, "done": action})
}

func mcpJSON(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	return string(b), err
}
