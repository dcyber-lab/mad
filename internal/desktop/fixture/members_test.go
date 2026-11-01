package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// What the panel lists: each heading with its rows (name, kind, state, whether tagged as the lead), and the line
// under the message box.
const panelJS = `(() => {
  const out = [];
  let cur = null;
  for (const e of document.querySelector("#chpanel .chside").children) {
    if (e.classList.contains("cpsec")) { cur = { title: e.textContent, rows: [] }; out.push(cur); }
    else if (cur && e.classList.contains("cplist")) {
      cur.rows = [...e.querySelectorAll(".cprow")].map((r) => ({
        name: r.querySelector(".nm").textContent, kind: r.querySelector(".k")?.textContent || "", st: r.dataset.st, lead: !!r.querySelector(".tag"),
        line: r.querySelector(".s").textContent, face: r.querySelector('[role="img"]')?.getAttribute("aria-label") || "", off: r.disabled,
      }));
    }
  }
  // No card clips what is in it, and the panel reaches its last row.
  const side = document.querySelector("#chpanel .chside");
  const clips = [];
  for (const card of side.querySelectorAll(".cplist")) {
    const cr = card.getBoundingClientRect();
    if (card.scrollHeight > card.clientHeight + 0.5) clips.push("card scrolls " + card.scrollHeight + ">" + card.clientHeight + ": " + card.textContent.slice(0, 30));
    for (const r of card.children) {
      const b = r.getBoundingClientRect();
      if (b.top < cr.top - 0.5 || b.bottom > cr.bottom + 0.5 || b.left < cr.left - 0.5 || b.right > cr.right + 0.5) {
        clips.push("row outside its card: " + r.textContent.slice(0, 30) + " " + Math.round(b.top) + ".." + Math.round(b.bottom) + " in " + Math.round(cr.top) + ".." + Math.round(cr.bottom));
      }
    }
  }
  side.scrollTop = side.scrollHeight;
  const last = [...side.querySelectorAll(".cpact")].pop();
  if (last && last.getBoundingClientRect().bottom > side.getBoundingClientRect().bottom + 0.5) clips.push("the panel does not reach its last row");
  side.scrollTop = 0;
  return {
    clips,
    sections: out, who: document.querySelector("#who").textContent,
    head: document.querySelector("#chanhead .chwhat small")?.textContent || "",
    dot: !!document.querySelector('.row[data-key^="t:"] .rdot[aria-label="needs you"]'),
  };
})()`

type panelRow struct {
	Name, Kind, St string
	Lead           bool
	Line, Face     string // what the row says of its state, and its character's label
	Off            bool   // the row is dim (no agent to open)
}

type panelReport struct {
	Sections []struct {
		Title string
		Rows  []panelRow
	}
	Clips []string
	Who   string
	Head  string
	Dot   bool
}

// openPanel opens channel id of the fixture at url in a window width wide, with its panel, and says what
// the panel lists.
func openPanel(t *testing.T, c *cdp, url string, width int, shot string) panelReport {
	t.Helper()
	if _, err := c.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": 900, "deviceScaleFactor": 1, "mobile": false}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.call("Page.navigate", map[string]any{"url": "about:blank"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.call("Page.navigate", map[string]any{"url": url}); err != nil {
		t.Fatal(err)
	}
	if !waitFor(20*time.Second, func() bool {
		v, err := c.eval(`document.querySelectorAll("#chanlog .cmsg").length`)
		n, _ := strconv.Atoi(string(v))
		return err == nil && n >= 1
	}) {
		t.Fatal("the channel did not draw")
	}
	if _, err := c.eval(`openChanPanel()`); err != nil {
		t.Fatal(err)
	}
	// Settled: the panel is open and nothing that ends is still running (its slide). What loops (a working
	// character) is not waited for.
	const settled = `(() => document.querySelector("#chpanel").classList.contains("open") &&
		document.getAnimations().every((a) => a.playState !== "running" || a.effect.getComputedTiming().iterations === Infinity))()`
	if !waitFor(10*time.Second, func() bool {
		v, err := c.eval(settled)
		return err == nil && string(v) == "true"
	}) {
		t.Fatal("the panel did not settle")
	}
	v, err := c.eval(panelJS)
	if err != nil {
		t.Fatal(err)
	}
	var rep panelReport
	if err := json.Unmarshal(v, &rep); err != nil {
		t.Fatal(err)
	}
	if shot != "" {
		if res, err := c.call("Page.captureScreenshot", map[string]any{"format": "png"}); err == nil {
			var s struct{ Data []byte }
			if json.Unmarshal(res, &s) == nil {
				_ = os.WriteFile(shot, s.Data, 0o644)
			}
		}
	}
	return rep
}

// A team's channel lists its members in two groups: 小组, the team's roles in their order with the lead
// first, and 帮手, the others, the busy ones first and a helper whose agent is gone left out. Any other
// channel is one list. Where there is no Chrome, it is skipped.
func TestPanelSplitsTheTeamFromItsHelpers(t *testing.T) {
	bin := chrome()
	if bin == "" {
		t.Skip("no Chrome: the panel's groups are not checked (MAD_CHROME names one)")
	}
	team, err := newHandler("team-members", "")
	if err != nil {
		t.Fatal(err)
	}
	fold, err := newHandler("channel-fold", "")
	if err != nil {
		t.Fatal(err)
	}
	teamSrv, foldSrv := httptest.NewServer(team), httptest.NewServer(fold)
	defer teamSrv.Close()
	defer foldSrv.Close()
	c := startChrome(t, bin)
	if _, err := c.call("Page.enable", nil); err != nil {
		t.Fatal(err)
	}
	tok := "#token=" + strings.Repeat("0", 32)
	shots := os.Getenv("MAD_BOUNDS_SHOTS")
	for _, w := range []int{390, 1600} {
		t.Run(fmt.Sprintf("team/%d", w), func(t *testing.T) {
			shot := ""
			if shots != "" {
				shot = filepath.Join(shots, fmt.Sprintf("members-%d.png", w))
			}
			rep := openPanel(t, c, teamSrv.URL+"/?channel=team"+tok, w, shot)
			if len(rep.Sections) < 3 {
				t.Fatalf("sections: %+v", rep.Sections)
			}
			for _, c := range rep.Clips {
				t.Errorf("clipped: %s", c)
			}
			team, helpers := rep.Sections[0], rep.Sections[1]
			if team.Title != "小组 5" || helpers.Title != "帮手 3" {
				t.Errorf("headings %q and %q, want 小组 5 and 帮手 3", team.Title, helpers.Title)
			}
			// A role with no agent yet says 未启动, one whose agent is gone 已离开: both dim, with a still character
			// that counts in the 小组 number. The others' lines are the agents' own.
			wantNew := []panelRow{
				{Name: "designer", St: "", Line: "未启动", Face: "stopped", Off: true},
				{Name: "tester", Kind: "claude", St: "", Line: "已离开", Face: "stopped", Off: true},
			}
			for i := range team.Rows {
				if i < 3 {
					team.Rows[i].Line, team.Rows[i].Face = "", ""
				}
			}
			if len(team.Rows) == 5 {
				if got := team.Rows[3:]; !reflect.DeepEqual(got, wantNew) {
					t.Errorf("roles without an agent: %+v, want %+v", got, wantNew)
				}
				team.Rows = team.Rows[:3]
			}
			for i := range helpers.Rows {
				helpers.Rows[i].Line, helpers.Rows[i].Face = "", ""
			}
			wantTeam := []panelRow{{Name: "architect", Kind: "claude", St: "idle", Lead: true}, {Name: "builder", Kind: "codex", St: "idle"}, {Name: "reviewer", Kind: "claude", St: "idle"}}
			if !reflect.DeepEqual(team.Rows, wantTeam) {
				t.Errorf("小组: %+v, want %+v", team.Rows, wantTeam)
			}
			wantHelpers := []panelRow{{Name: "ops", St: "running"}, {Name: "kim", St: "waiting"}, {Name: "docs", St: "idle"}}
			if !reflect.DeepEqual(helpers.Rows, wantHelpers) {
				t.Errorf("帮手: %+v, want %+v (the busy first, ghost, whose agent is gone, left out)", helpers.Rows, wantHelpers)
			}
			if rep.Sections[2].Title != "这个群" {
				t.Errorf("third heading %q", rep.Sections[2].Title)
			}
			if !strings.Contains(rep.Who, "3 个 agent · 3 帮手") {
				t.Errorf("under the message box: %q", rep.Who)
			}
			// What is done and what waits counts everyone, helpers too: kim, a helper, needs the person and ops, another,
			// works. The head says so, and the channel's row has the red dot.
			if rep.Head != "1 个在等你 · 1 个在工作" {
				t.Errorf("the head says %q, want 1 个在等你 · 1 个在工作", rep.Head)
			}
			if !rep.Dot {
				t.Error("the channel's row has no \"needs you\" dot, though a helper waits on the person")
			}
		})
		t.Run(fmt.Sprintf("no team/%d", w), func(t *testing.T) {
			rep := openPanel(t, c, foldSrv.URL+"/?channel=fold"+tok, w, "")
			if len(rep.Sections) < 2 || rep.Sections[0].Title != "成员 · 8" || len(rep.Sections[0].Rows) != 8 {
				t.Errorf("a channel without a team is one list of 8: %+v", rep.Sections)
			}
			for _, c := range rep.Clips {
				t.Errorf("clipped: %s", c)
			}
			for _, s := range rep.Sections {
				if strings.HasPrefix(s.Title, "小组") || strings.HasPrefix(s.Title, "帮手") {
					t.Errorf("a channel without a team has the heading %q", s.Title)
				}
			}
		})
	}
}
