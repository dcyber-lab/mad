package run

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/deck"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/notify"
	"github.com/dcyber-lab/mad/internal/poke"
	"github.com/dcyber-lab/mad/internal/state"
	"github.com/dcyber-lab/mad/internal/status"
	"github.com/dcyber-lab/mad/internal/textutil"
	"github.com/dcyber-lab/mad/internal/tmux"
	"github.com/dcyber-lab/mad/internal/transcript"
)

// Timing, replaceable in tests.
var (
	retryEvery = 2 * time.Second // a role that can't take a message yet is tried again
	paintEvery = time.Second     // the panel is redrawn
	usageEvery = 3 * time.Second // what the roles cost is read again
	compactFor = 5 * time.Minute // a compaction has this long to finish
	maxAsks    = 5               // questions a step may ask
	now        = time.Now
)

var (
	errCancelled = errors.New("cancelled")
	errResend    = errors.New("send the step again")
)

// What the run waits for you about, which decides what continue does.
const (
	waitAgent   = "agent"   // the agent asks you something: continue does nothing
	waitNoReply = "noreply" // the turn ended without a reply: continue sends again
	waitHold    = "hold"    // a gate, a review out of rounds: continue goes on
	waitLimit   = "limit"   // quota or budget: continue lets the step through
)

type runner struct {
	mu    sync.Mutex
	id    string
	proj  string // the project's directory
	pname string
	run   *state.Run
	dir   string
	f     *File
	flow  Flow
	kinds []agent.Kind
	ncfg  notify.Config
	out   io.Writer

	reader *transcript.Reader
	usage  map[string]transcript.Info // agent id → what its session says
	roleOf map[string]string          // agent id → its role
	panes  []tmux.Pane                // the deck's, as usage last read them
	lastUs time.Time
	diff   []string // git diff --stat, once done

	waitKind string
	notified string // the last reason you were told about
	cont     bool   // you pressed continue
	limitOK  bool   // let through past a limit for this step
}

// Exec is `mad run exec <id>`: run id's runner, in its own pane. It picks
// the run up where it stopped, so one started again (after a crash, or a
// deck rebuilt) goes on; one whose run is over just draws the panel.
func Exec(id string, out io.Writer) error {
	st, err := state.Load()
	if err != nil {
		return err
	}
	p, r := st.FindRun(id)
	if r == nil {
		return fmt.Errorf("no run %s", id)
	}
	dir := Dir(r)
	// One runner a run: another one started (the sidebar's, before it
	// saw this one) leaves, and takes its pane with it.
	lock, err := os.OpenFile(filepath.Join(dir, "runner.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		if pane := os.Getenv("TMUX_PANE"); pane != "" {
			_ = tmux.Run("kill-pane", "-t", pane)
		}
		return nil
	}
	f, err := Load(dir)
	if err != nil {
		return err
	}
	flow := f.Flow()
	if len(flow.Steps) == 0 {
		return fmt.Errorf("no flow %q", f.Spec.Flow)
	}
	kinds, _ := agent.Load()
	if kinds == nil {
		kinds = agent.Builtin()
	}
	// The panel's pane takes no keys: raw, a stray Ctrl-S can't stop its
	// output (and with it the runner), nor Ctrl-C or Ctrl-Z end it.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		if old, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
			defer term.Restore(int(os.Stdin.Fd()), old)
		}
	}
	cfg, _ := deck.LoadConfig()
	x := &runner{id: id, proj: p.Path, pname: p.Name, run: r, dir: dir, f: f, flow: flow, kinds: kinds,
		ncfg: cfg.Notify, out: out, reader: transcript.NewReader(), usage: map[string]transcript.Info{}}

	// The panel is redrawn every second, and at once when the pane is
	// resized (shown on stage, the terminal dragged).
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	defer signal.Stop(resized)
	stop := make(chan struct{})
	painted := make(chan struct{})
	go func() {
		defer close(painted)
		for {
			x.takeNotes()
			x.paint()
			select {
			case <-stop:
				return
			case <-resized:
			case <-time.After(paintEvery):
			}
		}
	}()
	err = x.work()
	close(stop)
	<-painted
	if err != nil {
		x.logf("failed: %v", err)
	}
	x.paint()
	// In its pane, the runner of a run that is over stays to keep the
	// panel drawn to the pane's size; a pane left dead would keep the
	// lines it had, wrapped anew at every resize.
	if f, ok := out.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		// Nothing changes now but the pane's size: drawn anew when it
		// does, and once a minute should a resize go unnoticed.
		for {
			select {
			case <-resized:
			case <-time.After(time.Minute):
			}
			x.paint()
		}
	}
	return err
}

func (x *runner) work() error {
	pr := &x.f.Progress
	if !Active(pr.Status) {
		if pr.Status == Done {
			x.setDiff()
		}
		return nil
	}
	x.update(func() {
		if pr.Started.IsZero() {
			pr.Started = now()
		}
		if pr.Round == 0 {
			pr.Round = 1
		}
		x.migrateRounds()
		pr.Status, pr.Waiting = Running, ""
	})
	// Every role up front, so what they ask at their start (a folder to
	// trust, an update) is settled before the first step.
	for _, role := range x.flow.Roles {
		if _, err := x.agent(role); err != nil {
			return x.fail(err)
		}
	}
	for _, role := range x.flow.Roles {
		if err := x.ready(role); errors.Is(err, errCancelled) {
			return x.cancelled()
		} else if err != nil {
			return x.fail(err)
		}
	}
	for {
		x.mu.Lock()
		i := pr.Step
		x.mu.Unlock()
		if i < 0 || i >= len(x.flow.Steps) {
			break
		}
		s := x.flow.Steps[i]
		reply, err := x.step(s)
		if errors.Is(err, errCancelled) {
			return x.cancelled()
		}
		if err != nil {
			return x.fail(err)
		}
		next := i + 1
		if s.Review {
			switch verdict(reply) {
			case Changes:
				x.mu.Lock()
				round := pr.roundOf(s.Name)
				x.mu.Unlock()
				limit := s.MaxRounds
				if limit == 0 {
					limit = x.f.Spec.MaxRounds
				}
				if round >= limit {
					if err := x.hold(fmt.Sprintf("still CHANGES after %d rounds: read the review, take over, or press c for another round", round)); err != nil {
						return x.cancelled()
					}
				}
				x.update(func() {
					if pr.Rounds == nil {
						pr.Rounds, pr.Reviews = map[string]int{}, map[string]string{}
					}
					pr.Round, pr.Review, pr.BackFrom = pr.Round+1, body(reply), s.Name
					pr.Rounds[s.Name], pr.Reviews[s.Name] = round+1, body(reply)
				})
				next = x.flow.StepIndex(s.Back)
			case "":
				if err := x.hold("the review gave no VERDICT: read its reply, press c to take it as approved"); err != nil {
					return x.cancelled()
				}
			}
		}
		if s.Gate && x.f.Spec.Gate {
			if err := x.hold(fmt.Sprintf("the %s is done: look it over (files in %s/), change what you like, then press c", s.Label, RelDir(x.run))); err != nil {
				return x.cancelled()
			}
		}
		x.update(func() { pr.Step = next })
	}
	x.update(func() { pr.Status, pr.Waiting, pr.Ended = Done, "", now() })
	x.setDiff()
	x.logf("run done")
	x.tell(notify.Done, "")
	return nil
}

// step has role s.Role take step s, and returns its reply.
func (x *runner) step(s Step) (string, error) {
	role, ok := x.flow.Role(s.Role)
	if !ok {
		return "", fmt.Errorf("flow %s: step %s has no role %s", x.flow.Name, s.Name, s.Role)
	}
	x.takeNotes() // before this step counts as under way: they were said before it
	pr := &x.f.Progress
	x.mu.Lock()
	x.limitOK = false
	n := len(pr.Entries)
	round, _ := x.roundOf(s)
	resumed := n > 0 && pr.Entries[n-1].Step == s.Name && pr.Entries[n-1].Round == round && pr.Entries[n-1].Status == "running"
	if !resumed {
		pr.Entries = append(pr.Entries, Entry{Step: s.Name, Role: s.Role, Round: round, Status: "running", Start: now()})
	}
	idx := len(pr.Entries) - 1
	sent := pr.Entries[idx].Sent
	x.mu.Unlock()
	x.save()

	a, err := x.agent(role)
	if err != nil {
		return "", err
	}
	x.readUsage(true)
	before := x.usageOf(a.ID)
	text := x.prompt(s, role)
	main := x.checkout()
	if !sent {
		x.logf("%s starts the %s (round %d)", role.Label, s.Label, round)
		if err := x.compactIfBig(a, role); err != nil {
			return "", err
		}
		if err := x.send(a, role, text); err != nil {
			return "", err
		}
		x.update(func() { pr.Entries[idx].Sent = true })
	}
	reply, err := x.converse(a, role, text)
	if err != nil {
		return "", err
	}
	if moved := changes(main, x.checkout()); len(moved) > 0 {
		x.logf("the main checkout changed during the %s (%s): a run works in its worktree alone, so if this was not you, look at what the %s did",
			s.Label, strings.Join(moved, ", "), role.Label)
	}
	x.readUsage(true)
	after := x.usageOf(a.ID)
	file := filepath.Join("steps", fmt.Sprintf("%02d-%s-%d.md", idx+1, s.Name, round))
	_ = os.MkdirAll(filepath.Join(x.dir, "steps"), 0o755)
	_ = os.WriteFile(filepath.Join(x.dir, file), []byte(reply+"\n"), 0o644)
	st := "done"
	if s.Review {
		st = strings.ToLower(verdict(reply))
		if st == "" {
			st = "done"
		}
	}
	x.update(func() {
		e := &pr.Entries[idx]
		e.Status, e.End, e.Reply, e.Result = st, now(), file, summary(reply)
		if s.Review && verdict(reply) != "" {
			e.Result = verdict(reply) + ifNotEmpty(": ", firstFinding(reply))
		}
		e.Cost = after.Tokens.Cost - before.Tokens.Cost
		if e.Cost == 0 {
			e.Tokens = after.Tokens.Total() - before.Tokens.Total()
		}
	})
	x.logf("%s finished the %s: %s", role.Label, s.Label, pr.Entries[idx].Result)
	return reply, nil
}

// setDiff takes in what the run changed, for the panel to show; the
// panel is drawn meanwhile.
func (x *runner) setDiff() {
	d := diffStat(x.run.Dir, x.f.Spec.Base)
	x.mu.Lock()
	x.diff = d
	x.mu.Unlock()
}

func ifNotEmpty(sep, s string) string {
	if s == "" {
		return ""
	}
	return sep + s
}

// firstFinding is the first line of a review that says something.
func firstFinding(reply string) string {
	b := body(reply)
	if b == "" {
		return ""
	}
	return summary(b)
}

// prompt is the message for step s.
func (x *runner) prompt(s Step, role Role) string {
	x.mu.Lock()
	round, review := x.roundOf(s)
	x.mu.Unlock()
	tpl := s.Prompt
	if round > 1 && (s.Review || x.backTarget(s.Name)) {
		switch {
		case s.Again != "":
			tpl = s.Again
		case s.Review:
			tpl = defaultRereview
		default:
			tpl = defaultFix
		}
	}
	head := fmt.Sprintf("[mad run %s · %s] You are the %s of this run: agents take the task in turns, and hand it on through the files in %s/.",
		x.run.Name, s.Name, role.Label, RelDir(x.run))
	if x.run.Dir != x.proj {
		head += fmt.Sprintf(" The run works in the worktree %s: change nothing outside it.", x.run.Dir)
	}
	head += " Write in the language the task is written in.\n\n"
	x.takeNotes()
	head += x.newNotes(role.Name)
	return head + render(string(tpl), map[string]string{
		"task": x.f.Spec.Task, "run": RelDir(x.run), "base": x.f.Spec.Base,
		"review": review, "round": strconv.Itoa(round - 1),
	}) + x.flow.ending(s)
}

// roundOf is the round step s is in and the findings it works from: a
// review's own, or for the step a review sends back to, those of the
// review that sent it. The caller holds x.mu.
func (x *runner) roundOf(s Step) (int, string) {
	pr := &x.f.Progress
	if s.Review {
		return pr.roundOf(s.Name), pr.Reviews[s.Name]
	}
	if i := x.flow.StepIndex(pr.BackFrom); i >= 0 && x.flow.Steps[i].Back == s.Name {
		return pr.roundOf(pr.BackFrom), pr.Reviews[pr.BackFrom]
	}
	return 1, ""
}

// migrateRounds gives a run from before reviews counted their own
// rounds the count of the review it is at.
func (x *runner) migrateRounds() {
	pr := &x.f.Progress
	if pr.Rounds != nil || pr.Round <= 1 {
		return
	}
	cur := ""
	if pr.Step >= 0 && pr.Step < len(x.flow.Steps) {
		cur = x.flow.Steps[pr.Step].Name
	}
	for i, s := range x.flow.Steps {
		if s.Review && (s.Back == cur || cur == "" || i >= pr.Step) {
			pr.Rounds, pr.Reviews = map[string]int{s.Name: pr.Round}, map[string]string{s.Name: pr.Review}
			pr.BackFrom = s.Name
			return
		}
	}
}

// backTarget: a review sends its changes back to step name.
func (x *runner) backTarget(name string) bool {
	for _, s := range x.flow.Steps {
		if s.Review && s.Back == name {
			return true
		}
	}
	return false
}

// converse waits for the reply to the message just sent to a, putting
// any question it ends in to the role it asks and the answer back to a.
func (x *runner) converse(a *state.Agent, role Role, text string) (string, error) {
	asked := 0
	for {
		reply, err := x.await(a, role, text)
		if err != nil {
			return "", err
		}
		target, q, ok := question(reply)
		other, known := x.flow.Role(target)
		if !ok || !known || other.Name == role.Name || asked >= maxAsks {
			return reply, nil
		}
		asked++
		x.logf("%s asks the %s: %s", role.Label, other.Label, q)
		answer, err := x.ask(other, role, q)
		if err != nil {
			return "", err
		}
		x.logf("%s answers: %s", other.Label, summary(answer))
		text = fmt.Sprintf("[mad run %s · answer] The %s answers:\n%s\n\nGo on with your task, and end it as asked before.", x.run.Name, other.Label, body(answer))
		if err := x.send(a, role, text); err != nil {
			return "", err
		}
	}
}

// ask puts a question from one role to another and returns the answer.
func (x *runner) ask(to, from Role, q string) (string, error) {
	a, err := x.agent(to)
	if err != nil {
		return "", err
	}
	text := fmt.Sprintf("[mad run %s · question] The %s asks you, while at work:\n%s\n\nAnswer it directly, change no files, and make the last line just DONE.", x.run.Name, from.Label, q)
	if err := x.send(a, to, text); err != nil {
		return "", err
	}
	return x.await(a, to, text)
}

// ready waits until role's agent can take a message.
func (x *runner) ready(role Role) error {
	for {
		if err := x.tick(); err != nil && !errors.Is(err, errResend) {
			return err
		}
		a, err := x.agent(role)
		if err != nil {
			return err
		}
		st, err := state.Load()
		if err != nil {
			return err
		}
		if _, cur := st.FindAgent(a.ID); cur != nil {
			a = cur
		}
		err = drive.Ready(st, a, x.kinds)
		var ask *drive.AskingError
		var draft *drive.DraftError
		switch {
		case err == nil, errors.Is(err, drive.ErrBusy):
			x.clearWait()
			return nil
		case errors.As(err, &ask):
			x.waitFor(fmt.Sprintf("the %s asks you something at its start%s: select it in the sidebar, press enter", role.Label, paren(ask.Question)), waitAgent)
		case errors.As(err, &draft):
			x.waitFor(x.draftReason(role), waitAgent)
		default:
			return err
		}
		time.Sleep(retryEvery)
	}
}

// send gets text to a, waiting while a can't take it: working, asking
// you something, or held up by the run's limits.
func (x *runner) send(a *state.Agent, role Role, text string) error {
	for {
		if err := x.tick(); err != nil && !errors.Is(err, errResend) {
			return err
		}
		if reason, budget := x.blocked(a, role); reason != "" {
			x.waitFor(reason, waitLimit)
			ok, err := x.takeContinue()
			if err != nil {
				return err
			}
			if ok {
				x.mu.Lock()
				x.limitOK = true
				if budget {
					x.f.Spec.Budget += DefaultBudget
				}
				x.mu.Unlock()
				x.save()
				continue
			}
			time.Sleep(retryEvery)
			continue
		}
		st, err := state.Load()
		if err != nil {
			return err
		}
		_, cur := st.FindAgent(a.ID)
		if cur == nil {
			if cur, err = x.agent(role); err != nil {
				return err
			}
		}
		err = drive.Send(st, cur, text, x.kinds)
		var ask *drive.AskingError
		var draft *drive.DraftError
		switch {
		case err == nil:
			x.clearWait()
			return nil
		case errors.As(err, &ask):
			x.waitFor(fmt.Sprintf("the %s asks you something%s: select it in the sidebar, press enter", role.Label, paren(ask.Question)), waitAgent)
		case errors.As(err, &draft):
			x.waitFor(x.draftReason(role), waitAgent)
		case errors.Is(err, drive.ErrBusy):
			// Running on its own (claude's Stop hooks, something you typed).
		default:
			return err
		}
		time.Sleep(retryEvery)
	}
}

// draftReason: what you typed into role's prompt is in the way of the
// run's next message, which waits for it to be sent or cleared.
func (x *runner) draftReason(role Role) string {
	return fmt.Sprintf("you have typed into the %s and not sent it: send it (the whole run will hear of it) or clear it, and the run goes on", role.Label)
}

// await waits for the end of a's turn and returns its reply. A turn that
// ends without one waits for you: continue sends text again, and a turn
// you finish in the agent counts too.
func (x *runner) await(a *state.Agent, role Role, text string) (string, error) {
	for {
		turn, err := drive.Wait(a, x.kinds, drive.WaitOptions{
			Note: func(asking bool, q string) {
				if !asking {
					x.clearWait()
					return
				}
				// What it is about to do says more than claude's own words.
				x.readUsage(true)
				if tool := x.usageOf(a.ID).Tool; tool != "" {
					q = tool
				}
				x.waitFor(fmt.Sprintf("the %s waits for your approval%s: select it in the sidebar, press enter", role.Label, paren(q)), waitAgent)
			},
			Tick: x.tick,
		})
		switch {
		case err == nil && turn != nil:
			x.clearWait()
			return turn.Reply, nil
		case err == nil, errors.Is(err, drive.ErrNoReply):
			x.waitFor("the "+role.Label+" stopped without a reply (interrupted?): step in, or press c to send the step again", waitNoReply)
		case errors.Is(err, errResend):
			x.clearWait()
			x.logf("sending it to the %s again", role.Label)
			if err := x.send(a, role, text); err != nil {
				return "", err
			}
		case errors.Is(err, errCancelled):
			return "", err
		case strings.Contains(err.Error(), "exited"):
			x.waitFor("the "+role.Label+" exited: press c to start it again and resend the step", waitNoReply)
			for {
				if err := x.tick(); errors.Is(err, errResend) {
					break
				} else if err != nil {
					return "", err
				}
				time.Sleep(retryEvery)
			}
			x.clearWait()
			if err := x.send(a, role, text); err != nil {
				return "", err
			}
		default:
			return "", err
		}
	}
}

// hold waits for you to continue, saying why.
func (x *runner) hold(reason string) error {
	x.mu.Lock()
	x.cont = false
	x.mu.Unlock()
	x.waitFor(reason, waitHold)
	for {
		ok, err := x.takeContinue()
		if err != nil {
			return err
		}
		if ok {
			break
		}
		time.Sleep(retryEvery / 4)
	}
	x.clearWait()
	return nil
}

// tick takes a command from the sidebar: cancel ends whatever waits,
// continue sends a step again that got no reply, or lets a hold go.
func (x *runner) tick() error {
	switch takeCommand(x.dir) {
	case Cancel:
		return errCancelled
	case Continue:
		x.mu.Lock()
		defer x.mu.Unlock()
		if x.waitKind == waitNoReply {
			return errResend
		}
		x.cont = true
	}
	return nil
}

// takeContinue reports whether you pressed continue since it last did.
func (x *runner) takeContinue() (bool, error) {
	if err := x.tick(); errors.Is(err, errCancelled) {
		return false, err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	c := x.cont
	x.cont = false
	return c, nil
}

// blocked says why a message to a should wait: its account's five-hour
// window nearly spent, or the run past its budget (overBudget). Continue
// lets it through for the step.
func (x *runner) blocked(a *state.Agent, role Role) (reason string, overBudget bool) {
	x.mu.Lock()
	ok, cost, budget := x.limitOK, x.f.Progress.Cost, x.f.Spec.Budget
	x.mu.Unlock()
	if ok {
		return "", false
	}
	if budget > 0 && cost >= budget {
		return fmt.Sprintf("%s spent, the budget is %s: press c for %s more", textutil.USD(cost), textutil.USD(budget), textutil.USD(DefaultBudget)), true
	}
	q, known := status.ReadQuota(role.Kind)
	if !known {
		q = x.usageOf(a.ID).Quota
	}
	if w := q.Expire(now()).FiveHour; w.Used >= 90 {
		until := ""
		if !w.ResetAt.IsZero() {
			until = ", resets in " + textutil.Until(w.ResetAt, now())
		}
		return fmt.Sprintf("%s's 5-hour window is %.0f%% used%s: press c to go on anyway", role.Kind, w.Used, until), false
	}
	return "", false
}

// compactIfBig compacts a claude role whose context grew past the run's
// limit before it takes the next step.
func (x *runner) compactIfBig(a *state.Agent, role Role) error {
	if role.Kind != "claude" || x.f.Spec.Compact <= 0 {
		return nil
	}
	ctx := x.contextOf(a)
	if ctx < x.f.Spec.Compact {
		return nil
	}
	x.logf("the %s has %s of context: compacting it first", role.Label, kTokens(ctx))
	sent := now()
	if err := x.send(a, role, "/compact"); err != nil {
		return err
	}
	for end := now().Add(compactFor); now().Before(end); time.Sleep(retryEvery / 4) {
		if err := x.tick(); err != nil && !errors.Is(err, errResend) {
			return err
		}
		if h := status.ReadHook(a.ID); h != nil && h.Event == "SessionStart" && !h.At.Before(sent) {
			x.logf("the %s is compacted: %s", role.Label, kTokens(x.contextOf(a)))
			return nil
		}
	}
	x.logf("the %s did not finish compacting in time; going on", role.Label)
	return nil
}

// agent is the agent playing role, spawned when it has none (yet, or any
// more). One that came up asking something is returned: sending to it
// waits for you.
func (x *runner) agent(role Role) (*state.Agent, error) {
	st, err := state.Load()
	if err != nil {
		return nil, err
	}
	if p := st.FindProject(x.proj); p != nil {
		for _, a := range p.Agents {
			if a.Run == x.id && a.Role == role.Name {
				return a, nil
			}
		}
	}
	x.logf("starting the %s (%s)", role.Label, roleAgent(x.kinds, role))
	a, err := drive.Spawn(drive.Spec{Kind: role.Kind, Name: x.run.Name + "-" + role.Name, Dir: x.run.Dir,
		Model: role.Model, Args: append(role.Args(x.f.Spec.Permission), role.WorktreeArgs(x.run.Dir)...), Run: x.id, Role: role.Name}, x.kinds)
	if a == nil {
		return nil, err
	}
	if err != nil {
		x.logf("the %s asks you something at its start: %v", role.Label, err)
	}
	return a, nil
}

// waitFor puts the run in waiting for you, telling you once per reason.
func (x *runner) waitFor(reason, kind string) {
	x.mu.Lock()
	pr := &x.f.Progress
	changed := pr.Waiting != reason
	pr.Status, pr.Waiting, x.waitKind = Waiting, reason, kind
	if changed {
		x.cont = false // a continue before this was for something else
	}
	tell := reason != x.notified
	x.notified = reason
	x.mu.Unlock()
	if changed {
		x.save()
		x.logf("waiting for you: %s", reason)
	}
	if tell {
		x.tell(notify.Waiting, reason)
	}
}

func (x *runner) clearWait() {
	x.mu.Lock()
	pr := &x.f.Progress
	was := pr.Status == Waiting
	if was {
		pr.Status, pr.Waiting = Running, ""
	}
	x.waitKind, x.notified = "", ""
	x.mu.Unlock()
	if was {
		x.save()
	}
}

func (x *runner) tell(kind, msg string) {
	if !x.ncfg.Wants(kind) {
		return
	}
	_ = notify.Send(x.ncfg, notify.Event{Kind: kind, Project: x.pname, Agent: "run " + x.run.Name, Message: msg})
}

func (x *runner) cancelled() error {
	x.update(func() {
		pr := &x.f.Progress
		pr.Status, pr.Waiting, pr.Ended = Cancelled, "", now()
	})
	x.logf("run cancelled; its branch and files stay")
	return nil
}

func (x *runner) fail(err error) error {
	x.update(func() {
		pr := &x.f.Progress
		pr.Status, pr.Waiting, pr.Ended = Failed, err.Error(), now()
	})
	return err
}

func (x *runner) update(f func()) {
	x.mu.Lock()
	f()
	x.mu.Unlock()
	x.save()
}

func (x *runner) save() {
	x.mu.Lock()
	err := x.f.Save(x.dir)
	x.mu.Unlock()
	if err == nil {
		_ = poke.Send(poke.Poll)
	}
}

func (x *runner) logf(format string, args ...any) {
	appendLog(x.dir, fmt.Sprintf(format, args...), now())
}

// readUsage brings what every role's session cost up to date; without
// force, at most every usageEvery. It reports whether the run's total
// changed.
func (x *runner) readUsage(force bool) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if !force && time.Since(x.lastUs) < usageEvery {
		return false
	}
	x.lastUs = time.Now()
	st, err := state.Load()
	if err != nil {
		return false
	}
	p := st.FindProject(x.proj)
	if p == nil {
		return false
	}
	var agents []transcript.Agent
	for _, a := range p.RunAgents(x.id) {
		agents = append(agents, transcript.Agent{ID: a.ID, Kind: a.Kind, Dir: p.Dir(a), Session: session(a)})
	}
	x.usage = x.reader.Read(agents)
	x.panes, _ = listPanes() // how the roles' panes stand, for the panel
	x.roleOf = map[string]string{}
	ctx := map[string]int64{}
	for _, a := range p.RunAgents(x.id) {
		x.roleOf[a.ID] = a.Role
		if c := lastContext(a.Kind, p.Dir(a), session(a)); c > 0 {
			ctx[a.Role] = c
		}
	}
	x.f.Progress.Context = ctx
	// The run's total is its roles' sessions, the step under way too.
	var cost float64
	var tokens int64
	for _, info := range x.usage {
		if info.Tokens.Cost > 0 {
			cost += info.Tokens.Cost
		} else {
			tokens += info.Tokens.Total()
		}
	}
	pr := &x.f.Progress
	changed := cost-pr.Cost >= 0.01 || tokens != pr.Tokens
	pr.Cost, pr.Tokens = cost, tokens
	return changed
}

func (x *runner) usageOf(id string) transcript.Info {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.usage[id]
}

func (x *runner) contextOf(a *state.Agent) int64 {
	return lastContext(a.Kind, a.Dir, session(a))
}

// session is the conversation an agent is on.
func session(a *state.Agent) string {
	if h := status.ReadHook(a.ID); h != nil && h.SessionID != "" {
		return h.SessionID
	}
	if a.SessionID != "" {
		return a.SessionID
	}
	return a.ID
}

// lastContext is how many tokens the session's last response read: its
// context as it stands.
func lastContext(kind, dir, sid string) int64 {
	p := discover.Lookup(kind)
	if p == nil {
		return 0
	}
	files := p.Transcripts(dir, sid)
	if len(files) == 0 {
		return 0
	}
	return contexts.look(kind, files[0])
}

// contexts keeps, for each transcript looked at, how far it was read and
// the context its last response read. Transcripts only grow, so a look
// reads what was added since the last one: nothing, mostly.
var contexts = &contextCache{marks: map[string]contextMark{}}

type contextCache struct {
	mu    sync.Mutex
	marks map[string]contextMark
}

type contextMark struct {
	read int64 // where the next look starts: after the last whole line
	last int64
}

func (c *contextCache) look(kind, path string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	m := c.marks[path]
	switch {
	case fi.Size() == m.read:
		return m.last
	case fi.Size() < m.read: // written anew
		m = contextMark{}
	}
	f, err := os.Open(path)
	if err != nil {
		return m.last
	}
	defer f.Close()
	if m.read == 0 && fi.Size() > 1<<20 {
		m.read = fi.Size() - 1<<20 // the first look: the tail says it
	}
	if _, err := f.Seek(m.read, io.SeekStart); err != nil {
		return m.last
	}
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // a line still being written is read whole next time
		}
		m.read += int64(len(line))
		if n, ok := lineContext(kind, line); ok {
			m.last = n
		}
	}
	c.marks[path] = m
	return m.last
}

// lineContext is the context a transcript line says a response read.
func lineContext(kind string, line []byte) (int64, bool) {
	switch {
	case kind == "claude" && bytes.Contains(line, []byte(`"usage"`)):
		var ln struct {
			Message struct {
				Usage *struct {
					In int64 `json:"input_tokens"`
					W  int64 `json:"cache_creation_input_tokens"`
					R  int64 `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &ln) == nil && ln.Message.Usage != nil {
			u := ln.Message.Usage
			return u.In + u.W + u.R, true
		}
	case kind == "codex" && bytes.Contains(line, []byte(`"last_token_usage"`)):
		var ln struct {
			Payload struct {
				Info struct {
					Last struct {
						In int64 `json:"input_tokens"`
					} `json:"last_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &ln) == nil && ln.Payload.Info.Last.In > 0 {
			return ln.Payload.Info.Last.In, true
		}
	}
	return 0, false
}

func kTokens(n int64) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return strconv.FormatInt(n, 10)
}

func paren(s string) string {
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}

// roleAgent names the agent a role is played by: its kind, or model,
// and its effort.
func roleAgent(kinds []agent.Kind, r Role) string {
	s := agent.ByName(kinds, r.Kind).Glyph() + " " + r.Kind
	if r.Model != "" {
		s = agent.ByName(kinds, r.Kind).Glyph() + " " + r.Model
	}
	if r.Effort != "" {
		s += " · " + r.Effort
	}
	return s
}

// diffStat is what the run changed, as git diff --stat puts it.
func diffStat(dir, base string) []string {
	out, err := gitOut(dir, "diff", "--stat", base)
	if err != nil || out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}
