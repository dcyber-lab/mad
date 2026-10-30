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
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/mcp"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
)

// mcpCmd serves mad's tools to the agent that started it, over stdio
// (`mad mcp --agent <id>`, put on its command line by deck.MCPFlags): the
// bus, to talk with the deck's other agents and the person.
func mcpCmd(args []string, stdio IO) error {
	fs := flags("mcp", stdio.Err)
	id := fs.String("agent", "", "the `id` of the agent it serves")
	resumed := fs.String("resumed", "", "the protocol `version` of a session an older mad began (set when it replaces itself)")
	if err := fs.Parse(args); err != nil || *id == "" || fs.NArg() > 0 {
		return errUsage
	}
	srv := &mcp.Server{Name: "mad", Version: version(), Instructions: mcpInstructions, Tools: mcpTools(*id), Serial: true, Resumed: *resumed}
	return srv.Serve(context.Background(), mcpUpgrade(srv, args, stdio.In), stdio.Out)
}

const mcpInstructions = `mad's bus: talk with the other agents in this deck, and with the person, in channels and direct messages.
Messages for you arrive as a turn that starts with [mad bus]. Only agents you @mention are interrupted; the rest read when they look.
Post results, questions and decisions; never acknowledgments or progress chatter. If you ask something, end your turn: the answer arrives as a new message.
To give work to another agent use bus_handoff, then end your turn; when you receive a handoff, finish it with bus_done.
The person is told about @you in a channel and a DM to "you": use them for what needs the person (a decision, an approval), not for updates.`

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
			Description: "Create a channel for a piece of work several agents share, or join, leave or invite to one. Reuse a channel that exists (bus_who lists them). The person is in every channel. The agent that creates a channel is its lead (mad nudges the lead when work stalls); lead makes another agent the lead (members[0], or me for yourself).",
			InputSchema: obj([]string{"action", "channel"}, `"action":{"type":"string","enum":["create","join","leave","invite","lead"]},"channel":{"type":"string","description":"its name (create) or id"},"topic":{"type":"string"},"members":{"type":"array","items":{"type":"string"},"description":"agents' labels or ids (create, invite; lead: the one agent, or me)"}`),
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
			InputSchema: obj([]string{"to", "task"}, `"to":{"type":"string","description":"an agent's label or id, or a kind to start: claude, claude:sonnet, codex, codex:gpt-5"},"task":{"type":"string","description":"what to do and what done looks like"},"context":{"type":"string","description":"what it must know: decisions, constraints, what you tried"},"files":{"type":"array","items":{"type":"string"},"description":"paths it should read or change"},"channel":{"type":"string","description":"post it in this channel instead of a DM"},"task_id":{"type":"string","description":"a task on the board (t-xxxxxxxx) this hands over, instead of a new one"}`),
			Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a struct {
					To, Task, Context, Channel string
					Files                      []string
					TaskID                     string `json:"task_id"`
				}
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				return mcpHandoff(id, a.To, a.Task, a.Context, a.Files, a.Channel, a.TaskID)
			},
		},
		{
			Name:        "skill_save",
			Description: "Save a way of working that proved itself as a skill others load later. description says when to use it (one or two sentences, the trigger words a person would say); body is markdown: the steps, exact commands and checks, the pitfalls you hit. Leave out secrets, machine paths and this conversation's specifics. It is pending until the person approves it.",
			InputSchema: obj([]string{"name", "description", "body"}, `"name":{"type":"string","description":"short, lower case, dashes: becomes the skill's directory and /name"},"description":{"type":"string","description":"when to use it, at most 400 characters"},"body":{"type":"string","description":"markdown: the steps"},"scope":{"type":"string","enum":["project","user"],"description":"project (default): for this project; user: general"},"replace":{"type":"boolean","description":"overwrite a skill of that name that mad did not install"}`),
			Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a struct {
					Name, Description, Body, Scope string
					Replace                        bool
				}
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				return mcpSkillSave(id, a.Name, a.Description, a.Body, a.Scope, a.Replace)
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
		{
			Name:        "task_list",
			Description: "The deck's task board: who owns what and how it stands. mine (default) is what you own, including handoffs you received. Call it when you start a turn with several things open, or before taking new work. Reading needs no reply.",
			InputSchema: obj([]string{}, `"scope":{"type":"string","enum":["mine","channel","all"],"description":"mine by default"},"channel":{"type":"string","description":"for scope channel"}`),
			Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a struct{ Scope, Channel string }
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				return mcpTaskList(id, a.Scope, a.Channel)
			},
		},
		{
			Name:        "task_set",
			Description: "Create a task (no id) or change one. Keep the board true in a few words: doing when you start, review when a PR or diff waits for someone, done with a one-line note when finished, dropped with why. A handoff you give or finish updates its task by itself (bus_handoff, bus_done); do not mirror those. Set owner only to give work to an agent that already knows about it (it is not woken): to relay work use bus_handoff.",
			InputSchema: obj([]string{}, `"id":{"type":"string","description":"the task to change, t-xxxxxxxx; omit to create"},"title":{"type":"string"},"owner":{"type":"string","description":"an agent's label or id, or \"you\""},"status":{"type":"string","enum":["todo","doing","review","done","dropped"]},"channel":{"type":"string"},"note":{"type":"string"},"links":{"type":"array","items":{"type":"object","required":["kind","ref"],"properties":{"kind":{"type":"string","enum":["pr","file","url"]},"ref":{"type":"string"}}}},"waits_on":{"type":"array","items":{"type":"string"},"description":"task ids this waits for"},"parent":{"type":"string","description":"the task this is a part of"}`),
			Handler: func(_ context.Context, raw json.RawMessage) (string, error) {
				var a struct {
					ID                   string
					Title, Owner, Status *string
					Channel, Note        *string
					Links                *[]bus.Link
					WaitsOn              *[]string `json:"waits_on"`
					Parent               *string
				}
				if err := json.Unmarshal(raw, &a); err != nil {
					return "", err
				}
				return mcpTaskSet(id, bus.TaskChange{ID: strings.TrimSpace(a.ID), Title: a.Title, Owner: a.Owner, Status: a.Status,
					Channel: a.Channel, Note: a.Note, Links: a.Links, WaitsOn: a.WaitsOn, Parent: a.Parent})
			},
		},
	}
}

// taskOfHandoff is the task of handoff id.
func taskOfHandoff(id string) (bus.Task, bool) {
	ts, _ := bus.Tasks()
	for _, t := range ts {
		if t.Handoff == id {
			return t, true
		}
	}
	return bus.Task{}, false
}

// mcpStatuses says how each agent stands, from what its hooks reported:
// cheap, unlike drive.Statuses.
func mcpStatuses(st *state.State) map[string]string {
	out := map[string]string{}
	for _, a := range st.OrderedAgents() {
		if h := status.ReadHook(a.ID); h != nil && h.State != "" {
			out[a.ID] = h.State
		}
	}
	return out
}

// mcpTaskList lists the tasks of the board as agent id sees them: its own,
// those of a channel, or all, each with how it stands.
func mcpTaskList(id, scope, channel string) (string, error) {
	_, st, err := mcpMe(id)
	if err != nil {
		return "", err
	}
	channel = strings.TrimPrefix(strings.TrimSpace(channel), "#")
	switch scope {
	case "":
		scope = "mine"
		if channel != "" {
			scope = "channel"
		}
	case "mine", "all":
	case "channel":
		if channel == "" {
			return "", errors.New("scope channel needs a channel")
		}
	default:
		return "", fmt.Errorf("scope is mine, channel or all, not %q", scope)
	}
	cards, err := bus.Board(st, mcpStatuses(st), time.Now())
	if err != nil {
		return "", err
	}
	type row struct {
		ID      string     `json:"id"`
		Title   string     `json:"title"`
		Owner   string     `json:"owner"`
		Status  string     `json:"status"`
		Derived string     `json:"derived"`
		Channel string     `json:"channel,omitempty"`
		Handoff string     `json:"handoff,omitempty"`
		Age     string     `json:"age"`
		Note    string     `json:"note,omitempty"`
		Links   []bus.Link `json:"links,omitempty"`
	}
	rows := []row{}
	for _, c := range cards {
		t := c.Task
		if t == nil || scope == "mine" && t.Owner != id || scope == "channel" && t.Channel != channel {
			continue
		}
		derived := c.Column
		if c.Why != "" {
			derived += ":" + c.Why
		}
		rows = append(rows, row{t.ID, t.Title, c.OwnerLabel, t.Status, derived, t.Channel, t.Handoff, mcpAge(c.Age), t.Note, t.Links})
	}
	b, err := json.Marshal(rows)
	return string(b), err
}

// mcpAge is a duration in the largest unit that fits: 5m, 3h, 2d.
func mcpAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// mcpTaskSet makes or changes a task as agent id; its owner is named by
// label or id, or "you".
func mcpTaskSet(id string, c bus.TaskChange) (string, error) {
	from, st, err := mcpMe(id)
	if err != nil {
		return "", err
	}
	if c.Owner != nil && *c.Owner != "" && !strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(*c.Owner), "@"), bus.You) {
		a, err := mcpAgent(st, *c.Owner)
		if err != nil {
			return "", err
		}
		c.Owner = &a.ID
	} else if c.Owner != nil && *c.Owner != "" {
		you := bus.You
		c.Owner = &you
	}
	if c.Channel != nil {
		ch := strings.TrimPrefix(strings.TrimSpace(*c.Channel), "#")
		c.Channel = &ch
	}
	t, err := bus.SetTask(from, c)
	if err != nil {
		return "", err
	}
	return mcpJSON(t)
}

// spawnAgent is drive.Spawn, replaceable in tests.
var spawnAgent = drive.Spawn

// mcpHandoff hands work from agent id to the agent to names, or to a new
// agent when to is a kind (name or name:model). A chain may start at most
// bus.MaxSpawns agents without a word from a person, and the deck
// at most bus.MaxOpenSpawned at work on handoffs at once.
func mcpHandoff(id, to, task, context string, files []string, channel, taskID string) (string, error) {
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
			return "", fmt.Errorf("%d agents were started for handoffs already without a word from the person; hand off to one that exists (bus_who), or ask the person", spawns)
		}
		if open, oerr := bus.OpenSpawned(); oerr != nil {
			return "", oerr
		} else if open >= bus.MaxOpenSpawned {
			return "", fmt.Errorf("%d agents started for handoffs are still at work; hand off to one that exists (bus_who), or ask the person", open)
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
	h, res, err := bus.HandOffTask(from, target.ID, task, context, files, channel, "", strings.TrimSpace(taskID))
	if err != nil {
		if spawned {
			err = fmt.Errorf("%v (%s was started and waits)", err, drive.Label(target))
		}
		return "", err
	}
	if spawned {
		_ = bus.MarkSpawned(h.ID)
	}
	tid := ""
	if t, ok := taskOfHandoff(h.ID); ok {
		tid = t.ID
	}
	return mcpJSON(map[string]any{
		"handoff": h.ID,
		"task":    tid,
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
		Lead    string   `json:"lead,omitempty"`
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
		outCh = append(outCh, channel{c.ID, c.Topic, members, unread[c.ID], c.Paused, label[c.Lead]})
	}
	type agent struct {
		ID       string   `json:"id"`
		Label    string   `json:"label"`
		Kind     string   `json:"kind"`
		Status   string   `json:"status"`
		Project  string   `json:"project"`
		Channels []string `json:"channels,omitempty"`
		Tasks    int      `json:"tasks"`
	}
	open := map[string]int{}
	ts, _ := bus.Tasks()
	for _, t := range ts {
		if t.Status != bus.TaskDone && t.Status != bus.TaskDropped {
			open[t.Owner]++
		}
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
			agents = append(agents, agent{a.ID, drive.Label(a), a.Kind, s, p.Name, chs, open[a.ID]})
		}
	}
	sort.SliceStable(agents, func(i, j int) bool { return agents[i].Label < agents[j].Label })
	return mcpJSON(map[string]any{"me": map[string]string{"id": me.ID, "label": me.Label}, "agents": agents, "channels": outCh, "skills": mcpSkillNames(st, id)})
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
		if action == "lead" && strings.EqualFold(strings.TrimSpace(m), "me") {
			ids = append(ids, id)
			continue
		}
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
	case "lead":
		lead := id
		if len(ids) > 0 {
			lead = ids[0]
		}
		if err = bus.SetLead(ch, lead); err == nil {
			return mcpJSON(map[string]any{"channel": ch, "done": action, "lead": lead})
		}
	default:
		return "", fmt.Errorf("no action %q: create, join, leave, invite or lead", action)
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

// mcpSkillSave queues a skill from agent id for the person to approve.
func mcpSkillSave(id, name, description, body, scope string, replace bool) (string, error) {
	me, st, err := mcpMe(id)
	if err != nil {
		return "", err
	}
	project := ""
	if p, _ := st.FindAgent(id); p != nil {
		project = p.Path
	}
	p, err := skill.Save(me.Label, project, scope, name, description, body, replace)
	if err != nil {
		return "", err
	}
	return mcpJSON(map[string]any{"pending": p.Path(), "name": p.Name, "note": "The person approves it before it is installed."})
}

// mcpSkillNames are the approved skills available to agent id.
func mcpSkillNames(st *state.State, id string) []string {
	project := ""
	if p, _ := st.FindAgent(id); p != nil {
		project = p.Path
	}
	names := []string{}
	for _, s := range skill.List(project) {
		names = append(names, s.Name)
	}
	return names
}
