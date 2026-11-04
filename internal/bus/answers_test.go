package bus

import (
	"math/rand"
	"testing"
)

// answeredScan is how a receipt's reply was found before answers: a scan
// of every later message.
func answeredScan(later []Message, m Message, id string, dm bool) bool {
	for _, x := range later {
		if x.From.ID != id || x.Kind == "system" {
			continue
		}
		if dm || x.ReplyTo == m.Seq || mentionsYou(x.Text) {
			return true
		}
	}
	return false
}

// answers finds what the scan found, on random channels.
func TestAnswersIsTheScan(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	who := []string{You, "a", "b", "c"}
	texts := []string{"ok", "@you look", "done", "@you?", "see @a"}
	kinds := []string{"text", "text", "text", "system", "handoff"}
	for round := 0; round < 300; round++ {
		var msgs []Message
		for seq := 1; seq <= 1+r.Intn(60); seq++ {
			m := Message{Seq: seq, From: From{ID: who[r.Intn(len(who))]}, Kind: kinds[r.Intn(len(kinds))], Text: texts[r.Intn(len(texts))]}
			if seq > 1 && r.Intn(3) == 0 {
				m.ReplyTo = 1 + r.Intn(seq-1)
			}
			msgs = append(msgs, m)
		}
		for _, dm := range []bool{false, true} {
			ans := answersIn(msgs, dm)
			for i, m := range msgs {
				for _, id := range who[1:] {
					if got, want := ans.answered(i, m, id), answeredScan(msgs[i+1:], m, id, dm); got != want {
						t.Fatalf("round %d, dm %v, message %d, %s: answers %v, scan %v", round, dm, m.Seq, id, got, want)
					}
				}
			}
		}
	}
}
