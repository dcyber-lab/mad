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
      }));
    }
  }
  return {
    sections: out, who: document.querySelector("#who").textContent,
    head: document.querySelector("#chanhead .chwhat small")?.textContent || "",
    dot: !!document.querySelector('.row[data-key^="t:"] .rdot[aria-label="needs you"]'),
  };
})()`

type panelRow struct {
	Name, Kind, St string
	Lead           bool
}

type panelReport struct {
	Sections []struct {
		Title string
		Rows  []panelRow
	}
	Who  string
	Head string
	Dot  bool
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
	for i := 0; i < 100; i++ {
		time.Sleep(150 * time.Millisecond)
		if v, err := c.eval(`document.querySelectorAll("#chanlog .cmsg").length`); err == nil {
			if n, _ := strconv.Atoi(string(v)); n >= 1 {
				break
			}
		}
	}
	if _, err := c.eval(`openChanPanel()`); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
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
			team, helpers := rep.Sections[0], rep.Sections[1]
			if team.Title != "小组 3" || helpers.Title != "帮手 3" {
				t.Errorf("headings %q and %q, want 小组 3 and 帮手 3", team.Title, helpers.Title)
			}
			wantTeam := []panelRow{{"architect", "claude", "idle", true}, {"builder", "codex", "idle", false}, {"reviewer", "claude", "idle", false}}
			if !reflect.DeepEqual(team.Rows, wantTeam) {
				t.Errorf("小组: %+v, want %+v", team.Rows, wantTeam)
			}
			wantHelpers := []panelRow{{"ops", "", "running", false}, {"kim", "", "waiting", false}, {"docs", "", "idle", false}}
			if !reflect.DeepEqual(helpers.Rows, wantHelpers) {
				t.Errorf("帮手: %+v, want %+v (the busy first, ghost, whose agent is gone, left out)", helpers.Rows, wantHelpers)
			}
			if rep.Sections[2].Title != "这个群" {
				t.Errorf("third heading %q", rep.Sections[2].Title)
			}
			if !strings.Contains(rep.Who, "3 agents · 3 帮手") {
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
			for _, s := range rep.Sections {
				if strings.HasPrefix(s.Title, "小组") || strings.HasPrefix(s.Title, "帮手") {
					t.Errorf("a channel without a team has the heading %q", s.Title)
				}
			}
		})
	}
}
