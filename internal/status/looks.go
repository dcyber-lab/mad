package status

// Tone is the color family a shown state is drawn in, by every surface:
// the window maps it to its CSS tokens, the deck to xterm-256 codes.
type Tone string

const (
	ToneWorking  Tone = "working"  // green
	ToneNeeds    Tone = "needs"    // amber
	ToneFinished Tone = "finished" // blue
	ToneQuiet    Tone = "quiet"    // grey
	ToneError    Tone = "error"    // red: only for something that broke
)

// Finished and Starting are shown states with no tracked status of their
// own: Finished is Idle or Asleep after a run the person did not watch,
// Starting is what a window-mode host reports while it comes up.
const (
	Finished = "finished"
	Starting = "starting"
)

// Look is how one shown state reads on every surface, so the person
// learns it once. The sidebar, tabs, composer header, deck and
// notifications take their word, glyph and color from here and nowhere
// else.
type Look struct {
	State   string `json:"state"`
	Word    string `json:"word"`
	Meaning string `json:"meaning"` // one plain line, for a tooltip
	Glyph   string `json:"glyph"`   // working's is the first spinner frame; the deck animates it
	Tone    Tone   `json:"tone"`
	// Also lists the raw statuses shown as this state, so no surface
	// keeps a mapping of its own.
	Also []string `json:"also,omitempty"`
}

// Looks are the shown states, most urgent first.
var Looks = []Look{
	{State: Waiting, Word: "Needs you", Glyph: "?", Tone: ToneNeeds,
		Meaning: "blocked on a permission or a question only you can answer"},
	{State: Running, Word: "Working", Glyph: "⠋", Tone: ToneWorking,
		Meaning: "working on a turn right now"},
	{State: Finished, Word: "Finished", Glyph: "●", Tone: ToneFinished,
		Meaning: "finished while you were away; clears when you look"},
	{State: Idle, Word: "Idle", Glyph: "○", Tone: ToneQuiet,
		Meaning: "waiting for your next message"},
	{State: Asleep, Word: "Asleep", Glyph: "◌", Tone: ToneQuiet, Also: []string{"off"},
		Meaning: "put to sleep to free what it held; wakes when you open it"},
	{State: Starting, Word: "Starting", Glyph: "…", Tone: ToneQuiet,
		Meaning: "its window-mode host is starting"},
	{State: Stopped, Word: "Stopped", Glyph: "·", Tone: ToneQuiet, Also: []string{Exited},
		Meaning: "not running: its process ended, or the deck restarted"},
}

// Shown is the state to draw for an agent whose tracked status is raw and
// who finished unseen when done. An unknown raw is shown as Stopped.
func Shown(raw string, done bool) string {
	if done && (raw == Idle || raw == Asleep) {
		return Finished
	}
	return LookOf(raw).State
}

// LookOf is the look of the shown state, or of the raw status folded into
// it.
func LookOf(s string) Look {
	for _, l := range Looks {
		if l.State == s {
			return l
		}
		for _, a := range l.Also {
			if a == s {
				return l
			}
		}
	}
	return Looks[len(Looks)-1]
}
