package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/notify"
)

// The spend gate: a daily bound on what mad starts by itself. Only turns
// mad began on its own count: a digest with no message from the person in
// it (a mention between agents, a nudge, a relay). The person's messages,
// the digests that carry one, and runs are never counted and never gated.
//
// bus/spend.json is the day's ledger. The deliverer (the sidebar's
// dispatcher) asks SpendOK before it takes a bus item, tells SpendSent
// when it sent one that counts, and the minute tick calls SpendSettle,
// which closes each open entry once the agent's turn has ended and adds
// what that turn cost (claude; codex and pi count turns only). A person
// typing into an agent mid-turn blends in; accepted.

// SpendWait is how long the dispatcher leaves an agent's held bus item
// before looking again: a raise lets it go within that.
const SpendWait = 5 * time.Second

// spendStale: an entry whose turn never ended (the agent was removed, or
// its hook is gone) is settled after this, so it cannot stay open.
const spendStale = 3 * time.Hour

// spendWarning is what an agent is told when it speaks to others with the
// gate closed.
const spendWarning = "today's budget for agent-to-agent work is spent; the person must raise it (mad bus budget). Your message is kept and no one is woken: finish the work yourself, or ask the person with @you"

// ErrSpent is why bus_handoff is refused with the gate closed.
var ErrSpent = errors.New("today's budget for agent-to-agent work is spent, so no one can be woken to take this: do not hand it off. Finish it yourself, or ask the person with @you to raise it (mad bus budget)")

// budget is the daily bound from config.json; replaceable in tests. A
// usd of 0 turns the dollar bound off, a turns of 0 the turn bound.
var budget = func() (usd float64, turns int) {
	cfg, _ := deck.LoadConfig()
	return cfg.Bus.BudgetUSD, cfg.Bus.BudgetTurns
}

// SpendOpen is a turn mad started and has not yet seen end.
type SpendOpen struct {
	Agent      string    `json:"agent"`
	SentAt     time.Time `json:"sent_at"`
	CostBefore float64   `json:"cost_before"`
}

// Spend is bus/spend.json.
type Spend struct {
	Day        string      `json:"day"`
	USD        float64     `json:"usd"`
	Turns      int         `json:"turns"`
	ExtraUSD   float64     `json:"extra_usd"`
	ExtraTurns int         `json:"extra_turns,omitempty"`
	OpenToday  bool        `json:"open_today"`
	Open       []SpendOpen `json:"open,omitempty"`
	ClosedAt   time.Time   `json:"closed_at,omitzero"`
	ToldAt     time.Time   `json:"told_at,omitzero"`
}

// SpendState is the ledger against the budget, for the window and the CLI.
type SpendState struct {
	Day         string    `json:"day"`
	USD         float64   `json:"usd"`
	Turns       int       `json:"turns"`
	BudgetUSD   float64   `json:"budget_usd"`   // with what was raised; 0 for no bound
	BudgetTurns int       `json:"budget_turns"` // likewise
	Opened      bool      `json:"open_today"`
	Closed      bool      `json:"closed"`
	ClosedAt    time.Time `json:"closed_at,omitzero"`
	// Part is how much of the budget is spent, the larger of the dollars'
	// and the turns' parts (1 = all); 0 with no bound. Hot: Part has
	// reached SpendWarn.
	Part float64 `json:"part"`
	Hot  bool    `json:"hot,omitempty"`
}

// SpendWarn is the part of the budget spent from which it shows as near:
// the spend bar turns amber, and it waits in the inbox.
const SpendWarn = 0.8

func spendFile() string { return filepath.Join(dir(), "spend.json") }

func dayOf(at time.Time) string { return at.Format("2006-01-02") }

// loadSpend reads the ledger as of time at: another day starts it afresh
// (the open turns stay, they settle onto the day they end in).
func loadSpend(at time.Time) Spend {
	var s Spend
	if data, err := os.ReadFile(spendFile()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if s.Day != dayOf(at) {
		s = Spend{Day: dayOf(at), Open: s.Open}
	}
	return s
}

func (s Spend) state() SpendState {
	usd, turns := budget()
	st := SpendState{Day: s.Day, USD: s.USD, Turns: s.Turns, Opened: s.OpenToday, ClosedAt: s.ClosedAt}
	if usd > 0 {
		st.BudgetUSD = usd + s.ExtraUSD
	}
	if turns > 0 {
		st.BudgetTurns = turns + s.ExtraTurns
	}
	over := st.BudgetUSD > 0 && s.USD >= st.BudgetUSD || st.BudgetTurns > 0 && s.Turns >= st.BudgetTurns
	st.Closed = over && !s.OpenToday
	if st.BudgetUSD > 0 {
		st.Part = s.USD / st.BudgetUSD
	}
	if st.BudgetTurns > 0 {
		st.Part = max(st.Part, float64(s.Turns)/float64(st.BudgetTurns))
	}
	st.Hot = st.Part >= SpendWarn
	return st
}

// SpendNow is how the day stands at time at.
func SpendNow(at time.Time) SpendState { return loadSpend(at).state() }

// SpendOK tells whether mad may start a turn by itself at time at: the
// day's budget is not spent, or the person opened it.
func SpendOK(at time.Time) bool { return !SpendNow(at).Closed }

// changeSpend runs fn on the ledger as of at under the bus lock and saves it.
func changeSpend(at time.Time, fn func(*Spend)) error {
	return locked(func() error {
		s := loadSpend(at)
		fn(&s)
		data, err := json.Marshal(s)
		if err != nil {
			return err
		}
		return writeAtomic(spendFile(), data)
	})
}

// SpendSent records that mad started a turn for agent id at time at, the
// agent's cost so far being costBefore. It is not called for the person's
// messages.
func SpendSent(id string, costBefore float64, at time.Time) error {
	return changeSpend(at, func(s *Spend) {
		for i, o := range s.Open {
			if o.Agent == id { // the earlier turn is over: its end was not seen yet
				s.USD += math.Max(0, costBefore-o.CostBefore)
				s.Turns++
				s.Open = append(s.Open[:i], s.Open[i+1:]...)
				break
			}
		}
		s.Open = append(s.Open, SpendOpen{Agent: id, SentAt: at, CostBefore: costBefore})
	})
}

// SpendSettle closes the turns mad started whose agent has ended its turn
// since (or that stood open for hours), adding their cost from costs, each
// agent's cost now (a missing agent costs nothing). When that closes the
// gate the person is told, once a day. The sidebar runs it every minute.
func SpendSettle(at time.Time, costs map[string]float64) error {
	msg := ""
	err := changeSpend(at, func(s *Spend) {
		var keep []SpendOpen
		for _, o := range s.Open {
			t := readTurn(o.Agent)
			if !(t != nil && t.At.After(o.SentAt)) && at.Sub(o.SentAt) < spendStale {
				keep = append(keep, o)
				continue
			}
			s.USD += math.Max(0, costs[o.Agent]-o.CostBefore)
			s.Turns++
		}
		s.Open = keep
		st := s.state()
		switch {
		case !st.Closed:
			s.ClosedAt = time.Time{}
		case s.ClosedAt.IsZero():
			s.ClosedAt = at
		}
		if st.Closed && s.ToldAt.IsZero() {
			s.ToldAt = at
			msg = fmt.Sprintf("mad spent $%.2f on its own today (%d turns); agents wait for you", s.USD, s.Turns)
		}
	})
	if msg != "" {
		tellPerson(notify.Budget, msg)
	}
	return err
}

// SpendRaise adds usd dollars to today's budget, and the same share of
// its turns (usd as a part of the dollar budget; usd turns when the
// dollar bound is off).
func SpendRaise(usd float64, at time.Time) error {
	if usd <= 0 {
		return fmt.Errorf("raise by a positive amount of dollars, not %v", usd)
	}
	bu, bt := budget()
	more := int(math.Ceil(usd))
	if bu > 0 {
		more = int(math.Ceil(usd / bu * float64(bt)))
	}
	return changeSpend(at, func(s *Spend) {
		s.ExtraUSD += usd
		if bt > 0 {
			s.ExtraTurns += more
		}
		s.ToldAt = time.Time{} // spent again, the person hears again
		s.ClosedAt = time.Time{}
	})
}

// SpendOpenToday lifts the bound until midnight.
func SpendOpenToday(at time.Time) error {
	return changeSpend(at, func(s *Spend) { s.OpenToday, s.ClosedAt = true, time.Time{} })
}

// PersonWaits tells whether what waits for agent id on the bus carries a
// message from the person: that digest is theirs, never held by the gate.
func PersonWaits(id string) bool {
	var list []entry
	_ = locked(func() error {
		var err error
		list, _, _, err = gather(id)
		return err
	})
	return fromPerson(list)
}

func fromPerson(list []entry) bool {
	for _, e := range list {
		if e.m.From.ID == You {
			return true
		}
	}
	return false
}

// Line is the day as one line: "$2.31 of $20 · 17 of 150 turns".
func (s SpendState) Line() string {
	line := fmt.Sprintf("$%.2f", s.USD)
	if s.BudgetUSD > 0 {
		line += " of $" + trimUSD(s.BudgetUSD)
	}
	line += fmt.Sprintf(" · %d", s.Turns)
	if s.BudgetTurns > 0 {
		line += fmt.Sprintf(" of %d", s.BudgetTurns)
	}
	return line + " turns"
}

func trimUSD(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.2f", v)
}
