package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
Post results, questions and decisions; never acknowledgments or progress chatter. If you ask something, end your turn: the answer arrives as a new message.
To give work to another agent use bus_handoff, then end your turn; when you receive a handoff, finish it with bus_done.`

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
		{
			Name:        "bus_handoff",
			Description: "Hand a piece of work to another agent, or to a new one: give an agent's label, or a kind (claude, claude:<model>, codex, codex:<model>) to start one in your directory. The receiver does not share your conversation: put everything it needs in task, context and files. Then end your turn; you are woken with its result when it finishes (bus_done), declines, or ends its turn without finishing.",
			InputSchema: obj([]string{"to", "task"}, `"to":{"type":"string","description":"an agent's label or id, or a kind to start: claude, claude:sonnet, codex, codex:gpt-5"},"task":{"type":"string","description":"what to do and what done looks like"},"context":{"type":"string","description":"what it must know: decisions, constraints, what you tried"},"files":{"type":"array","items":{"type":"string"},"description":"paths it should read or change"},"channel":{"type":"string","description":"post it in this channel instead of a DM"}`),
			Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a struct {
					To, Task, Context, Channel string
					Files                      []string
				}
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				return mcpHandoff(id, a.To, a.Task, a.Context, a.Files, a.Channel)
			},
		},
		{
			Name:        "bus_done",
			Description: "Finish a handoff you received: say what you did (what changed, where, what you checked), or why you decline (status \"declined\"). Call it once, as your last bus action, then end your turn; the agent that handed it over is woken with your result.",
			InputSchema: obj([]string{"handoff", "result"}, `"handoff":{"type":"string","description":"the handoff id, h-xxxxxxxx"},"result":{"type":"string"},"status":{"type":"string","enum":["done","declined"],"description":"done by default"}`),
			Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a struct{ Handoff, Result, Status string }
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				from, _, err := mcpMe(id)
				if err != nil {
					return "", err
				}
				h, res, err := bus.Done(from, strings.TrimSpace(a.Handoff), a.Result, a.Status)
				if err != nil {
					return "", err
				}
				return mcpJSON(map[string]any{"handoff": h.ID, "status": h.Status, "woke": res.Woke})
			},
		},
	}
}

// spawnAgent is drive.Spawn, replaceable in tests.
var spawnAgent = drive.Spawn

// mcpHandoff hands work from agent id to the agent to names, or to a new
// agent when to is a kind (name or name:model). A chain may start at most
// bus.MaxSpawns agents without a word from a person in the channel.
func mcpHandoff(id, to, task, context string, files []string, channel string) (string, error) {
	from, st, err := mcpMe(id)
	if err != nil {
		return "", err
	}
	channel = strings.TrimPrefix(strings.TrimSpace(channel), "#")
	var target *state.Agent
	spawned := false
	if target, err = mcpAgent(st, to); err != nil {
		kind, model, _ := strings.Cut(strings.TrimSpace(to), ":")
		known := false
		for _, k := range kinds() {
			known = known || k.Name == kind
		}
		if !known {
			return "", err
		}
		// Only these have the bus, to finish it with.
		if kind != "claude" && kind != "codex" {
			return "", fmt.Errorf("a new agent for a handoff is claude or codex (with :model), not %s", kind)
		}
		depth, spawns, cerr := bus.ChainState(id, "", channel)
		switch {
		case cerr != nil:
			return "", cerr
		case depth >= bus.MaxDepth:
			return "", fmt.Errorf("a chain of handoffs is at most %d deep; do the work yourself or finish yours with bus_done", bus.MaxDepth)
		case spawns >= bus.MaxSpawns:
			return "", fmt.Errorf("this chain has started %d agents already without a word from the person; hand off to one that exists (bus_who), or ask the person", spawns)
		}
		p, me := st.FindAgent(id)
		suffix := make([]byte, 2)
		_, _ = rand.Read(suffix)
		target, err = spawnAgent(drive.Spec{Kind: kind, Model: model, Dir: p.Dir(me), Name: kind + "-" + hex.EncodeToString(suffix)}, kinds())
		if target == nil {
			return "", err
		}
		spawned = true
		if err != nil { // it came up asking something: the person must answer
			return "", fmt.Errorf("started %s but it is not ready: %v", drive.Label(target), err)
		}
	}
	h, res, err := bus.HandOff(from, target.ID, task, context, files, channel, "")
	if err != nil {
		if spawned {
			err = fmt.Errorf("%v (%s was started and waits)", err, drive.Label(target))
		}
		return "", err
	}
	if spawned {
		_ = bus.MarkSpawned(h.ID)
	}
	return mcpJSON(map[string]any{
		"handoff": h.ID,
		"to":      map[string]any{"id": target.ID, "label": drive.Label(target), "spawned": spawned},
		"status":  h.Status,
		"woke":    res.Woke,
		"note":    "End your turn now; you are woken when it finishes.",
	})
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
