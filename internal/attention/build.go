package attention

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/status"
)

// build is the whole derivation, pure. Desk.Look is its only caller
// outside tests.
//
// The order of the steps is the order of precedence when two sources
// name the same key: an agent's own wait comes before a card about it.
func build(f Facts) View {
	b := builder{f: f, byKey: map[Key]int{}, byAgent: map[string][]int{}, known: map[string]bool{}, rank: map[string]int{}}
	for i, a := range f.Agents {
		b.known[a.ID] = true
		b.rank[a.ID] = i
	}
	for _, a := range f.Agents {
		b.agent(a)
	}
	for _, c := range f.Cards {
		b.card(c)
	}
	for _, p := range f.Skills {
		b.skill(p)
	}
	b.budget(f.Spend)
	needs := order(b.items)
	fin := b.finished()
	return View{
		Inbox:    Inbox{Items: append(append([]Item{}, needs...), fin...), Needs: len(needs)},
		Activity: activities(f.Agents),
		Working:  working(f.Agents),
		Board:    b.rest,
	}
}

// builder gathers items by key, merging what names the same thing.
type builder struct {
	f     Facts
	items []Item
	byKey map[Key]int // index into items
	// byAgent are the indices of an agent's blocking items, for a task
	// card to fold into; known are the agents among the facts.
	byAgent map[string][]int
	known   map[string]bool
	// rank is each agent's place in the deck's order: what equal times and
	// equal keys alike are ordered by, so every surface lists equal facts
	// alike.
	rank map[string]int
	rest []bus.Card
}

// rankOf is the place of agent id's items among equals: items of no agent
// after all agents'.
func (b *builder) rankOf(id string) int {
	if r, ok := b.rank[id]; ok {
		return r
	}
	return len(b.rank)
}

// add puts it in, or merges it into the item with its key: its tasks
// are joined, the earlier Since kept, the rest of the first item kept.
func (b *builder) add(it Item) {
	it.oneLine()
	it.rank = b.rankOf(it.Agent)
	it.Acts = it.route.acts()
	if it.Acts == nil {
		it.Acts = []Act{}
	}
	if i, ok := b.byKey[it.Key]; ok {
		x := &b.items[i]
		for _, t := range it.Tasks {
			x.addTask(t)
		}
		switch {
		case it.undated:
		case x.undated, it.Since.Before(x.Since):
			x.Since, x.undated = it.Since, false
		}
		return
	}
	b.byKey[it.Key] = len(b.items)
	if it.Blocks && it.Agent != "" {
		b.byAgent[it.Agent] = append(b.byAgent[it.Agent], len(b.items))
	}
	b.items = append(b.items, it)
}

// oneLine makes the words an item is drawn in one line each: a row is a
// line, however long the task note, the question or the hook's message.
// What Text had past its line (a note's list, a question's context) is
// kept in Body, which the page shows whole.
func (it *Item) oneLine() {
	full := strings.TrimSpace(it.Text)
	it.Text = headline(full)
	switch {
	case full == it.Text:
	case it.Body == "":
		it.Body = full
	default:
		it.Body = full + "\n\n" + it.Body
	}
	it.Title, it.Context, it.Where = flat(it.Title), flat(it.Context), flat(it.Where)
	for i := range it.Tasks {
		it.Tasks[i].Title = flat(it.Tasks[i].Title)
	}
}

// flat is s on one line, its whitespace collapsed.
func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

// headline is text as one line: its first line when the rest is a list (a
// heading and its points), else all of it, collapsed.
func headline(text string) string {
	first, rest, multi := strings.Cut(text, "\n")
	if !multi {
		return flat(text)
	}
	for _, l := range strings.Split(rest, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") || strings.HasPrefix(l, "• ") || numbered(l) {
			return flat(first)
		}
	}
	return flat(text)
}

// numbered: "1. " or "2) ".
func numbered(l string) bool {
	i := 0
	for i < len(l) && l[i] >= '0' && l[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(l) && (l[i] == '.' || l[i] == ')') && l[i+1] == ' '
}

func (it *Item) addTask(t TaskRef) {
	for _, x := range it.Tasks {
		if x.ID == t.ID {
			return
		}
	}
	it.Tasks = append(it.Tasks, t)
}

// alive tells whether a's process can be waiting on anything: a host's
// asks file is left behind by one that died.
func alive(a Agent) bool {
	return a.Status == status.Running || a.Status == status.Waiting || a.Status == status.Idle
}

// agent adds what agent a waits on the person for: its host's open
// requests (window mode), else one item when it waits (its TUI's prompt,
// told by its hook or its screen). A waiting agent always has an item,
// with a task or not.
func (b *builder) agent(a Agent) {
	if len(a.Report.Asks) > 0 && alive(a) {
		for _, ask := range a.Report.Asks {
			it := Item{Key: keyRequest(a.ID, ask.Host, ask.ID), Agent: a.ID, Title: a.Label, Since: ask.At, Blocks: true,
				Open: Open{Agent: a.ID}, route: hostRoute{agent: a.ID, ask: ask}}
			switch ask.Kind {
			case status.AskQuestion:
				it.Kind, it.Context, it.Text = KindQuestion, "asks", ask.Question
			case status.AskPlan:
				it.Kind, it.Context, it.Text = KindPermission, "plan", ask.Plan
			default:
				it.Kind, it.Context, it.Where, it.Body = KindPermission, ask.Tool, a.Dir, ask.Title
				it.Text, it.Mono = ask.Target, ask.Command != ""
				if ask.Command != "" {
					it.Text = ask.Command
					if ask.Target != ask.Command && it.Body == "" {
						it.Body = ask.Target
					}
				}
			}
			if it.Since.IsZero() {
				it.Since = a.Since
			}
			b.add(it)
		}
		return
	}
	if a.Status != status.Waiting {
		return
	}
	var at time.Time // a screen wait: no hook says when
	hook := a.Report.Hook
	if hook != nil && hook.State == status.Waiting {
		at = hook.At
	}
	// Its prompt answered by keys when its screen shows one that can be
	// read; else Open, never a guess.
	var route route = openRoute{label: "Open " + a.Label}
	p, keys := parsePrompt(a.Screen)
	if keys && a.PaneID != "" {
		route = keysRoute{pane: a.PaneID, p: p}
	} else {
		p = prompt{}
	}
	it := Item{Key: keyWait(a.ID, at, p.ID), Kind: KindPermission, Agent: a.ID, Title: a.Label, Where: a.Dir,
		Since: a.Since, Blocks: true, Open: Open{Agent: a.ID}, route: route}
	tool, target := splitCall(a.Info.Tool)
	switch {
	case tool == "AskUserQuestion" || tool == "request_user_input":
		it.Kind, it.Context, it.Text = KindQuestion, "asks", "a question"
	case tool != "":
		it.Context, it.Text, it.Mono = tool, target, target != ""
	default:
		it.Context = "needs you"
	}
	if hook != nil && hook.Message != "" {
		if it.Text == "" || it.Text == "a question" {
			it.Text = hook.Message
		} else {
			it.Body = hook.Message
		}
	}
	if p.Question != "" && (tool == "" || it.Kind == KindQuestion) {
		// What the screen asks says more than a hook's "needs your permission".
		it.Text, it.Body, it.Mono = p.Question, p.Detail, false
	}
	if it.Since.IsZero() {
		it.Since, it.undated = b.f.Now, true
	}
	b.add(it)
}

// card adds or folds a board card; cards outside waiting-you go to rest.
// In waiting-you, by kind and why:
//
//	ask                      → ask, keyMessage(channel, seq)
//	task, asked              → the same ask key, with the task in Tasks
//	paused                   → paused, keyPaused
//	task, paused             → the same paused key, with the task in Tasks
//	checkin                  → checkin, keyCheckin(timer)
//	task, permission         → folded into its waiting owner's items; with
//	                           its owner among the agents but not waiting
//	                           (the card is older than the statuses), dropped;
//	                           with its owner gone, a task item
//	task, the person's or unowned, anything else → a task item, Open the board
func (b *builder) card(c bus.Card) {
	if c.Column != bus.ColWaitingYou {
		b.rest = append(b.rest, c)
		return
	}
	since, undated := c.Since, c.Since.IsZero()
	if undated {
		since = b.f.Now
	}
	var tasks []TaskRef
	if c.Task != nil {
		tasks = []TaskRef{{ID: c.Task.ID, Title: c.Task.Title}}
	}
	switch {
	case c.Kind == bus.CardAsk && c.Message != nil, c.Kind == bus.CardTask && c.Why == bus.WhyAsked && c.Message != nil:
		text, body := c.Title, c.Message.Text
		asker, label := c.Owner, c.OwnerLabel
		if c.Kind == bus.CardTask { // the task's card has the whole message
			text, body = splitFirst(c.Message.Text)
		}
		where := "in #" + c.Channel
		if strings.HasPrefix(c.Channel, "dm.") {
			where = "in your DM"
			if r := (busRoute{channel: c.Channel}); r.theirs() {
				var names []string
				for _, s := range bus.DMSides(c.Channel) {
					names = append(names, b.nameOf(s))
				}
				where = "in " + strings.Join(names, " ↔ ") + "'s DM"
			}
		}
		b.add(Item{Key: keyMessage(c.Channel, c.Message.Seq), Kind: KindAsk, Agent: asker, Title: label, Context: where,
			Text: text, Body: body, Tasks: tasks, Since: since, undated: undated,
			Open: Open{Channel: c.Channel}, route: busRoute{channel: c.Channel, seq: c.Message.Seq}})
	case c.Kind == bus.CardPaused, c.Kind == bus.CardTask && c.Why == bus.WhyPaused && c.Channel != "":
		b.add(Item{Key: keyPaused(c.Channel), Kind: KindPaused, Icon: "pause", Title: "#" + c.Channel, Context: "paused",
			Text: "its agents' messages wait for you", Tasks: tasks, Since: since, undated: undated,
			Open: Open{Channel: c.Channel}, route: channelRoute{channel: c.Channel}})
	case c.Kind == bus.CardCheckin:
		label := c.OwnerLabel
		b.add(Item{Key: keyCheckin(c.Ref), Kind: KindCheckin, Agent: c.Owner, Icon: "clock", Title: label, Context: "check-in",
			Text: strings.TrimPrefix(c.Title, label+"'s check-in: "), Body: c.Note, Since: since, undated: undated,
			Open: Open{Agent: c.Owner}, route: timerRoute{timer: c.Ref, label: label}})
	case c.Kind == bus.CardTask && c.Why == bus.WhyPermission && c.Task != nil && b.known[c.Owner]:
		for _, i := range b.byAgent[c.Owner] {
			b.items[i].addTask(tasks[0])
		}
	case c.Task != nil:
		ctx := "yours"
		switch {
		case c.Why == bus.WhyUnowned:
			ctx = "unowned"
		case c.Why != "":
			ctx = c.Why
		}
		b.add(Item{Key: keyTask(c.Task.ID), Kind: KindTask, Icon: "task", Title: c.Title, Context: ctx, Text: c.Note,
			Tasks: tasks, Since: since, undated: undated, Open: Open{Page: PageBoard}, route: openRoute{label: "Open board"}})
	default:
		b.add(Item{Key: keyCard(c.Kind, c.Channel, c.Title), Kind: KindTask, Icon: "task", Title: c.Title, Context: c.Why,
			Text: c.Note, Since: since, undated: undated, Open: Open{Page: PageBoard}, route: openRoute{label: "Open board"}})
	}
}

// nameOf is agent id's name on the bus, or the id of one gone.
func (b *builder) nameOf(id string) string {
	for _, a := range b.f.Agents {
		if a.ID == id {
			return a.Label
		}
	}
	return id
}

// splitFirst is text's first line, and the rest.
func splitFirst(text string) (first, rest string) {
	first, rest, _ = strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(first), strings.TrimSpace(rest)
}

// skill adds a pending skill.
func (b *builder) skill(p skill.Pending) {
	b.add(Item{Key: keySkill(p.Name), Kind: KindSkill, Icon: "skill", Title: "/" + p.Name, Context: "skill from " + p.By,
		Text: p.Description, Since: p.At, Open: Open{Page: PageSkills}, route: skillRoute{slug: p.Name}})
}

// budget adds the day's budget once bus.SpendWarn of it is spent, or all;
// not once lifted for the day. Spent, agents wait on it.
func (b *builder) budget(s bus.SpendState) {
	if s.Opened || !s.Closed && s.Part < bus.SpendWarn {
		return
	}
	part := s.Part
	it := Item{Key: keyBudget(s.Day, s.Closed), Kind: KindBudget, Icon: "budget", Title: "Budget",
		Context: fmt.Sprintf("%.0f%% spent", part*100), Text: s.Line() + " today", Part: &part,
		Open: Open{Page: PageBoard}, route: budgetRoute{}}
	// SpendState has no "near since": the Desk keeps the first look's time.
	it.Since, it.undated = b.f.Now, true
	if s.Closed {
		it.Context, it.Blocks = "spent · agents wait", true
		if !s.ClosedAt.IsZero() {
			it.Since, it.undated = s.ClosedAt, false
		}
	}
	b.add(it)
}

// finished is an item per agent shown Finished, newest first.
func (b *builder) finished() []Item {
	var out []Item
	for _, a := range b.f.Agents {
		if a.Shown != status.Finished {
			continue
		}
		it := Item{Key: keyFinished(a.ID, a.Since), Kind: KindFinished, Agent: a.ID, Title: a.Label, Context: "finished",
			Since: a.Since, Open: Open{Agent: a.ID}, route: seenRoute{label: a.Label, pane: a.PaneID}}
		if r := replyOf(a); r != "" {
			it.Text = firstLine(r, replyRunes)
		}
		it.Acts = it.route.acts()
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Since.Equal(out[j].Since) {
			return out[i].Since.After(out[j].Since)
		}
		return less(out[i], out[j])
	})
	return out
}

// less breaks a tie: by the agent's place in the deck's order, then by key.
func less(a, b Item) bool {
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	return a.Key < b.Key
}

// order sorts what needs the person: Blocks first, then Since ascending
// (oldest first), then Key, so equal times keep one order everywhere.
func order(items []Item) []Item {
	out := append([]Item{}, items...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Blocks != b.Blocks {
			return a.Blocks
		}
		if !a.Since.Equal(b.Since) {
			return a.Since.Before(b.Since)
		}
		return less(a, b)
	})
	return out
}

// working is the ids of the agents shown working, longest in it first.
func working(agents []Agent) []string {
	var ws []Agent
	for _, a := range agents {
		if a.Shown == status.Running {
			ws = append(ws, a)
		}
	}
	// agents are in the deck's order, which a stable sort keeps among equals.
	// An unknown Since (zero) is not the longest: it goes last.
	sort.SliceStable(ws, func(i, j int) bool {
		a, b := ws[i].Since, ws[j].Since
		if a.IsZero() || b.IsZero() {
			return !a.IsZero() && b.IsZero()
		}
		return a.Before(b)
	})
	out := []string{}
	for _, a := range ws {
		out = append(out, a.ID)
	}
	return out
}
