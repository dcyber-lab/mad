package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// chrome finds a Chrome or Chromium to drive; MAD_CHROME names one.
func chrome() string {
	if p := os.Getenv("MAD_CHROME"); p != "" {
		return p
	}
	for _, p := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

// cdp is a Chrome DevTools session on one page: commands in, their results out (events are let go).
type cdp struct {
	conn *websocket.Conn
	ctx  context.Context
	id   int
}

func (c *cdp) call(method string, params any) (json.RawMessage, error) {
	c.id++
	id := c.id
	msg, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err := c.conn.Write(c.ctx, websocket.MessageText, msg); err != nil {
		return nil, err
	}
	for {
		_, b, err := c.conn.Read(c.ctx)
		if err != nil {
			return nil, err
		}
		var r struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(b, &r) != nil || r.ID != id {
			continue
		}
		if r.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, r.Error.Message)
		}
		return r.Result, nil
	}
}

// eval runs expression in the page and returns its value as JSON.
func (c *cdp) eval(expr string) (json.RawMessage, error) {
	res, err := c.call("Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true})
	if err != nil {
		return nil, err
	}
	var r struct {
		Result    struct{ Value json.RawMessage } `json:"result"`
		Exception *struct {
			Text, Description string
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, err
	}
	if r.Exception != nil {
		return nil, fmt.Errorf("page: %s %s", r.Exception.Text, r.Exception.Description)
	}
	return r.Result.Value, nil
}

// skipOrFail is for what stops the check from running although Chrome is there: it skips on a machine
// of someone's, and fails where CI is set, so a green CI is never green for nothing.
func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

// waitFor polls cond until it holds, up to timeout.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// removeProfile removes Chrome's profile directory, as well as it can: its helper processes may still be
// writing to it for a moment after the browser has gone, so it tries again; it never fails the test (a
// leftover temp directory is not what is being checked).
func removeProfile(dir string) {
	for i := 0; i < 25; i++ {
		if os.RemoveAll(dir) == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// getJSON reads the JSON of a GET at url into v.
func getJSON(url string, v any) error {
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return json.NewDecoder(res.Body).Decode(v)
}

// shutdown closes Chrome as it closes itself, over the DevTools protocol on the browser's own socket
// (Browser.close: its helper processes go with it), and waits for it to be gone; one that does not go
// within ten seconds is killed.
func shutdown(cmd *exec.Cmd, port string) {
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	if port != "" {
		var v struct {
			WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
		}
		if getJSON("http://127.0.0.1:"+port+"/json/version", &v) == nil && v.WebSocketDebuggerURL != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if conn, _, err := websocket.Dial(ctx, v.WebSocketDebuggerURL, nil); err == nil {
				_ = conn.Write(ctx, websocket.MessageText, []byte(`{"id":1,"method":"Browser.close"}`))
				_ = conn.CloseNow()
			}
			cancel()
		}
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

// startChrome runs a headless Chrome and returns a session on its first page.
func startChrome(t *testing.T, bin string) *cdp {
	t.Helper()
	// Not t.TempDir: its removal fails the test when Chrome's helpers are still writing. This runs last.
	dir, err := os.MkdirTemp("", "mad-chrome-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeProfile(dir) })
	cmd := exec.Command(bin, "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
		"--user-data-dir="+dir, "--remote-debugging-port=0", "--remote-allow-origins=*", "about:blank")
	if err := cmd.Start(); err != nil {
		skipOrFail(t, "chrome did not start: %v", err)
	}
	var port string
	t.Cleanup(func() { shutdown(cmd, port) })
	waitFor(10*time.Second, func() bool {
		b, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort"))
		if err != nil {
			return false
		}
		port = strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
		return port != ""
	})
	if port == "" {
		skipOrFail(t, "chrome did not say where it listens")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	var ws string
	waitFor(10*time.Second, func() bool {
		var list []struct{ Type, WebSocketDebuggerURL string }
		if getJSON("http://127.0.0.1:"+port+"/json/list", &list) == nil {
			for _, p := range list {
				if p.Type == "page" {
					ws = p.WebSocketDebuggerURL
				}
			}
		}
		return ws != ""
	})
	if ws == "" {
		skipOrFail(t, "chrome has no page")
	}
	conn, _, err := websocket.Dial(ctx, ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetReadLimit(32 << 20)
	t.Cleanup(func() { _ = conn.CloseNow() })
	return &cdp{conn: conn, ctx: ctx}
}

// What the page reports about bounds: every box of a message that reaches past its column or past the
// window, and the sideways scroll of the page and the log.
const boundsJS = `(() => {
  const log = document.querySelector("#chanlog");
  const lr = log.getBoundingClientRect();
  const cs = getComputedStyle(log);
  const left = lr.left + parseFloat(cs.paddingLeft), right = lr.right - parseFloat(cs.paddingRight);
  const bad = [];
  for (const e of log.querySelectorAll(".cmsg, .cmsg *, .cgrp, .cstack")) {
    if (e.closest("pre") && e !== e.closest("pre")) continue; // a code block scrolls inside its box
    const r = e.getBoundingClientRect();
    if (!r.width && !r.height) continue;
    if (r.right > right + 0.5 || r.left < left - 0.5 || r.right > innerWidth + 0.5) {
      bad.push((e.className || e.tagName) + " " + Math.round(r.left) + ".." + Math.round(r.right) + " of " + Math.round(left) + ".." + Math.round(right) + ": " + (e.textContent || "").slice(0, 40));
    }
  }
  const sideways = [];
  for (const [n, e] of [["html", document.documentElement], ["body", document.body], ["#chanscroll", document.querySelector("#chanscroll")], ["#chanlog", log]]) {
    if (e.scrollWidth > e.clientWidth + 0.5) sideways.push(n + " " + e.scrollWidth + ">" + e.clientWidth);
  }
  return { bad, sideways, messages: log.querySelectorAll(".cmsg").length, quotes: log.querySelectorAll(".cquote").length, code: log.querySelectorAll(".ctext code").length, pre: log.querySelectorAll(".ctext pre").length, panel: !document.querySelector("#chpanel").hidden };
})()`

// A message never reaches past its column, nor the window, at the widths of a desktop window with the
// 群信息 panel open and closed; no page or log scrolls sideways. The channel-fold fixture has the messages
// that broke it: a reply with a quote and inline code, a long word, a long link, a code block. Where there
// is no Chrome, it is skipped.
func TestChannelBubblesStayInTheColumn(t *testing.T) {
	bin := chrome()
	if bin == "" {
		t.Skip("no Chrome: the channel's bounds are not checked (MAD_CHROME names one)")
	}
	h, err := newHandler("channel-fold", "")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := startChrome(t, bin)
	if _, err := c.call("Page.enable", nil); err != nil {
		t.Fatal(err)
	}
	for _, w := range []int{390, 1240, 1600, 2000} {
		for _, panel := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/panel=%v", w, panel), func(t *testing.T) {
				if _, err := c.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": w, "height": 900, "deviceScaleFactor": 1, "mobile": false}); err != nil {
					t.Fatal(err)
				}
				if _, err := c.call("Page.navigate", map[string]any{"url": "about:blank"}); err != nil {
					t.Fatal(err)
				}
				url := srv.URL + "/?channel=fold#token=" + strings.Repeat("0", 32)
				if _, err := c.call("Page.navigate", map[string]any{"url": url}); err != nil {
					t.Fatal(err)
				}
				// Wait for the channel to be drawn: all its messages.
				var rep struct {
					Bad, Sideways               []string
					Messages, Quotes, Code, Pre int
					Panel                       bool
				}
				waitFor(20*time.Second, func() bool {
					v, err := c.eval(`document.querySelectorAll("#chanlog .cmsg").length`)
					if err != nil {
						return false
					}
					n, _ := strconv.Atoi(string(v))
					return n >= 9
				})
				if panel {
					if _, err := c.eval(`openChanPanel()`); err != nil {
						t.Fatal(err)
					}
				}
				// Settled: the panel is open (or not wanted), and nothing that ends is still running (the panel's slide, a
				// fade). What loops (a working character) is not waited for.
				settled := `(() => !!document.querySelector("#chpanel").classList.contains("open") === ` + strconv.FormatBool(panel) +
					` && document.getAnimations().every((a) => a.playState !== "running" || a.effect.getComputedTiming().iterations === Infinity))()`
				if !waitFor(10*time.Second, func() bool {
					v, err := c.eval(settled)
					return err == nil && string(v) == "true"
				}) {
					t.Fatalf("the page did not settle (panel open: %v)", panel)
				}
				v, err := c.eval(boundsJS)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(v, &rep); err != nil {
					t.Fatal(err)
				}
				if rep.Messages < 9 || rep.Quotes == 0 || rep.Code == 0 || rep.Pre == 0 || rep.Panel != panel {
					t.Fatalf("the channel did not draw as the check needs: %+v", rep)
				}
				if shot := os.Getenv("MAD_BOUNDS_SHOTS"); shot != "" {
					if res, err := c.call("Page.captureScreenshot", map[string]any{"format": "png"}); err == nil {
						var s struct{ Data []byte }
						if json.Unmarshal(res, &s) == nil {
							_ = os.WriteFile(filepath.Join(shot, fmt.Sprintf("bounds-%d-panel-%v.png", w, panel)), s.Data, 0o644)
						}
					}
				}
				for _, b := range rep.Bad {
					t.Errorf("reaches past its column: %s", b)
				}
				for _, s := range rep.Sideways {
					t.Errorf("scrolls sideways: %s", s)
				}
			})
		}
	}
}

// What the page shows of the receipt line under the person's messages: how many lines there are and under
// which message, and the latest one's word, colour, size, height, place against its bubble, title and label.
const receiptLineJS = `(() => {
  const lines = [...document.querySelectorAll("#chanlog .crcpt")];
  const l = lines[0];
  if (!l) return { count: 0 };
  const row = l.parentElement, b = row.querySelector(".cbub");
  const lr = l.getBoundingClientRect(), br = b.getBoundingClientRect();
  const cs = getComputedStyle(l);
  return { count: lines.length, seq: row.dataset.seq, word: l.textContent, blue: l.classList.contains("blue"), has: l.classList.contains("has"),
    color: cs.color, size: parseFloat(cs.fontSize), weight: cs.fontWeight, height: lr.height, dx: br.right - lr.right, dy: lr.top - br.bottom,
    title: l.title, label: l.getAttribute("aria-label") || "", mine: document.querySelectorAll("#chanlog .cmsg.mine").length };
})()`

// The receipts scenario has the person's messages, the latest with two agents who have it but have not read
// it, in a paused channel. Only that message shows the line, "2 个未读" in blue; clicking it opens the card of
// who, which Esc or a click outside closes. The line is patched in place (its word changes, the bubble stays),
// and a newer message of the person's takes the line, empty and 20px tall, in one pass. Every width and
// theme: the line and the card stay inside the column. MAD_BOUNDS_SHOTS=<dir> keeps a screenshot of each,
// with the card shut and open. Where there is no Chrome, it is skipped.
func TestReceiptLineAndCard(t *testing.T) {
	bin := chrome()
	if bin == "" {
		t.Skip("no Chrome: the receipts are not checked (MAD_CHROME names one)")
	}
	h, err := newHandler("receipts", "")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := startChrome(t, bin)
	if _, err := c.call("Page.enable", nil); err != nil {
		t.Fatal(err)
	}
	evalJSON := func(t *testing.T, expr string, into any) {
		t.Helper()
		v, err := c.eval(expr)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(v, into); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	shot := func(name string) {
		if dir := os.Getenv("MAD_BOUNDS_SHOTS"); dir != "" {
			if res, err := c.call("Page.captureScreenshot", map[string]any{"format": "png"}); err == nil {
				var s struct{ Data []byte }
				if json.Unmarshal(res, &s) == nil {
					_ = os.WriteFile(filepath.Join(dir, name+".png"), s.Data, 0o644)
				}
			}
		}
	}
	inColumn := func(t *testing.T) {
		t.Helper()
		var rep struct{ Bad, Sideways []string }
		evalJSON(t, boundsJS, &rep)
		for _, b := range rep.Bad {
			t.Errorf("reaches past its column: %s", b)
		}
		for _, s := range rep.Sideways {
			t.Errorf("scrolls sideways: %s", s)
		}
	}
	for _, theme := range []string{"light", "dark"} {
		for _, w := range []int{390, 1600} {
			t.Run(fmt.Sprintf("%s/%d", theme, w), func(t *testing.T) {
				if _, err := c.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": w, "height": 900, "deviceScaleFactor": 1, "mobile": false}); err != nil {
					t.Fatal(err)
				}
				if _, err := c.call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}}); err != nil {
					t.Fatal(err)
				}
				if _, err := c.call("Page.navigate", map[string]any{"url": "about:blank"}); err != nil {
					t.Fatal(err)
				}
				if _, err := c.call("Page.navigate", map[string]any{"url": srv.URL + "/?channel=fold#token=" + strings.Repeat("0", 32)}); err != nil {
					t.Fatal(err)
				}
				if !waitFor(20*time.Second, func() bool {
					v, err := c.eval(`document.querySelectorAll("#chanlog .crcpt.has").length`)
					return err == nil && string(v) == "1"
				}) {
					t.Fatal("the page did not draw the receipt line")
				}
				time.Sleep(1500 * time.Millisecond) // the fade-in, and a look or two at the channel
				var l struct {
					Count          int
					Seq, Word      string
					Blue, Has      bool
					Color          string
					Size           float64
					Weight         string
					Height, Dx, Dy float64
					Title, Label   string
					Mine           int
				}
				evalJSON(t, receiptLineJS, &l)
				blue := map[string]string{"light": "rgb(31, 107, 255)", "dark": "rgb(47, 123, 255)"}[theme]
				if l.Count != 1 || l.Seq != "9" || l.Mine < 4 {
					t.Errorf("want one line, under the latest of the person's messages (seq 9): %+v", l)
				}
				if l.Word != "2 个未读" || !l.Blue || l.Color != blue {
					t.Errorf("the line reads %q (blue=%v, colour %s), want 2 个未读 in %s", l.Word, l.Blue, l.Color, blue)
				}
				if l.Size != 12.5 || l.Weight != "400" || l.Height != 20 {
					t.Errorf("the line is %vpx, weight %s, %vpx tall; want 12.5, 400, 20", l.Size, l.Weight, l.Height)
				}
				if l.Dx < 3.5 || l.Dx > 4.5 || l.Dy < 3.5 || l.Dy > 4.5 {
					t.Errorf("the line is %vpx in from the bubble's right edge and %vpx below it; want 4 and 4", l.Dx, l.Dy)
				}
				if l.Title != "reviewer 已送达 · builder 已送达" && l.Title != "builder 已送达 · reviewer 已送达" {
					t.Errorf("title %q is not the breakdown", l.Title)
				}
				if !strings.Contains(l.Label, "builder 未读") || !strings.Contains(l.Label, "reviewer 未读") {
					t.Errorf("label %q is not the breakdown in words", l.Label)
				}
				inColumn(t)
				shot(fmt.Sprintf("receipts-%d-%s", w, theme))

				// the card
				if _, err := c.eval(`document.querySelector("#chanlog .crcpt").click()`); err != nil {
					t.Fatal(err)
				}
				var card struct {
					Rows           []string
					Foot           []string
					Dx             float64
					Left, Width    float64
					Expanded, Pops bool
				}
				evalJSON(t, `(() => {
				  const c = document.querySelector("#chanlog .rcard"), l = document.querySelector("#chanlog .crcpt");
				  if (!c) return { rows: [], foot: [] };
				  const cr = c.getBoundingClientRect(), lr = l.getBoundingClientRect();
				  return { rows: [...c.querySelectorAll(".rrow")].map((r) => r.querySelector(".rname").textContent + "|" + r.querySelector(".rstate").textContent + "|" + (r.querySelector(".avatar") ? "av" : "")),
				    foot: [...c.querySelectorAll(".rfoot")].map((x) => x.textContent), dx: lr.right - cr.right, left: cr.left, width: cr.width,
				    expanded: l.getAttribute("aria-expanded") === "true", pops: !!c.closest(".cmsg.mine") };
				})()`, &card)
				if len(card.Rows) != 2 || !strings.HasSuffix(card.Rows[0], "|已送达，还没看|av") || len(card.Foot) < 1 || card.Foot[0] != "只显示被 @ 到的 agent" {
					t.Errorf("the card is %+v", card)
				}
				if card.Dx < -1 || card.Dx > 1 || !card.Expanded || !card.Pops {
					t.Errorf("the card is not right-aligned under the line or the line does not say it is open: %+v", card)
				}
				if card.Left < 0 {
					t.Errorf("the card starts at %v, off the screen", card.Left)
				}
				inColumn(t)
				shot(fmt.Sprintf("receipts-%d-%s-card", w, theme))
				// Esc shuts it; so does a click outside
				for _, how := range []string{
					`document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }))`,
					`document.querySelector("#chanlog .cgrp").click()`,
				} {
					if _, err := c.eval(how); err != nil {
						t.Fatal(err)
					}
					v, _ := c.eval(`document.querySelectorAll("#chanlog .rcard").length`)
					if string(v) != "0" {
						t.Errorf("the card stayed after %s", how)
					}
					if _, err := c.eval(`document.querySelector("#chanlog .crcpt").click()`); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := c.eval(`document.querySelector("#chanlog .crcpt").click()`); err != nil { // shut it again
					t.Fatal(err)
				}

				// patched in place: the word changes, the bubble and the line stay the same nodes
				var patch struct {
					SameBubble, SameLine bool
					Word                 string
					Blue                 bool
				}
				evalJSON(t, `(() => {
				  const row = document.querySelector("#chanlog .cmsg.mine .crcpt").parentElement, bub = row.querySelector(".cbub"), line = row.querySelector(".crcpt");
				  drawReceipts([{ seq: 9, targets: [{ id: "x", label: "builder", state: "replied" }, { id: "y", label: "reviewer", state: "read" }] }], false);
				  return { sameBubble: bub === row.querySelector(".cbub") && bub.isConnected, sameLine: line === row.querySelector(".crcpt"), word: line.textContent, blue: line.classList.contains("blue") };
				})()`, &patch)
				if !patch.SameBubble || !patch.SameLine || patch.Word != "已回复" || patch.Blue {
					t.Errorf("the line was not patched in place to 已回复 in grey: %+v", patch)
				}
				// a newer message of the person's takes the line, empty and 20px tall, in the same pass
				var next struct {
					Lines      int
					Under      string
					Has        bool
					Height     float64
					OldHasNone bool
					Word       string
				}
				evalJSON(t, `(() => {
				  drawMessage({ seq: 10, at: Date.now(), from: { id: "you", label: "you" }, kind: "text", text: "再说一句" });
				  drawReceipts(cv.receipts, cv.paused);
				  const lines = [...document.querySelectorAll("#chanlog .crcpt")];
				  const l = lines[0];
				  return { lines: lines.length, under: l.parentElement.dataset.seq, has: l.classList.contains("has"), height: l.getBoundingClientRect().height,
				    oldHasNone: !document.querySelector('#chanlog .cmsg[data-seq="9"] .crcpt'), word: l.textContent };
				})()`, &next)
				if next.Lines != 1 || next.Under != "10" || next.Has || next.Height != 20 || !next.OldHasNone || next.Word != "" {
					t.Errorf("the new message did not take the line, reserved and empty: %+v", next)
				}
			})
		}
	}
}

// What agents settle between themselves is not in the channel: in channel-fold the handoff, the done report,
// the nudge, the check-in and the lapsed notice draw nothing; an @you result is an ordinary bubble; mad's
// resume note stays; a run of one sender is not split by a hidden message, and a hidden message adds no time
// line; a reply that quotes a hidden message shows its snippet. A channel with only such messages says that
// agents are collaborating in the background. At a phone's width and a wide window's, in both themes
// (MAD_BOUNDS_SHOTS=<dir> keeps a screenshot of each). Where there is no Chrome, it is skipped.
func TestQuietKindsAreNotDrawn(t *testing.T) {
	bin := chrome()
	if bin == "" {
		t.Skip("no Chrome: the channel is not checked (MAD_CHROME names one)")
	}
	fold, err := newHandler("channel-fold", "")
	if err != nil {
		t.Fatal(err)
	}
	quietH, err := newHandler("quiet", "")
	if err != nil {
		t.Fatal(err)
	}
	foldSrv, quietSrv := httptest.NewServer(fold), httptest.NewServer(quietH)
	defer foldSrv.Close()
	defer quietSrv.Close()
	c := startChrome(t, bin)
	if _, err := c.call("Page.enable", nil); err != nil {
		t.Fatal(err)
	}
	open := func(t *testing.T, base string, w int, theme string) {
		t.Helper()
		if _, err := c.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": w, "height": 900, "deviceScaleFactor": 1, "mobile": false}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.call("Page.navigate", map[string]any{"url": "about:blank"}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.call("Page.navigate", map[string]any{"url": base + "/?channel=fold#token=" + strings.Repeat("0", 32)}); err != nil {
			t.Fatal(err)
		}
	}
	shot := func(name string) {
		if dir := os.Getenv("MAD_BOUNDS_SHOTS"); dir != "" {
			// the end of the channel, once what rises has risen
			for i := 0; i < 3; i++ {
				_, _ = c.eval(`(() => { for (const e of [document.querySelector("#chanscroll"), document.querySelector("#chanlog"), document.scrollingElement]) e.scrollTop = 1e9; })()`)
				time.Sleep(400 * time.Millisecond)
			}
			if res, err := c.call("Page.captureScreenshot", map[string]any{"format": "png"}); err == nil {
				var s struct{ Data []byte }
				if json.Unmarshal(res, &s) == nil {
					_ = os.WriteFile(filepath.Join(dir, name+".png"), s.Data, 0o644)
				}
			}
		}
	}
	for _, theme := range []string{"light", "dark"} {
		for _, w := range []int{390, 1600} {
			t.Run(fmt.Sprintf("%s/%d", theme, w), func(t *testing.T) {
				open(t, foldSrv.URL, w, theme)
				if !waitFor(20*time.Second, func() bool {
					v, err := c.eval(`document.querySelector('#chanlog [data-seq="21"]') ? 1 : 0`)
					return err == nil && string(v) == "1"
				}) {
					t.Fatal("the channel did not draw")
				}
				var rep struct {
					Seqs       []string
					Sys        []string
					Times      int
					Lines      int
					RunNames   int
					Quote      string
					Text       string
					Bounds     struct{ Bad, Sideways []string }
					AtYouWhole bool
				}
				v, err := c.eval(`(() => {
				  const log = document.querySelector("#chanlog");
				  const bounds = (` + boundsJS + `);
				  const r15 = log.querySelector('[data-seq="15"]'), r17 = log.querySelector('[data-seq="17"]');
				  return { seqs: [...log.querySelectorAll(".cmsg")].map((e) => e.dataset.seq), sys: [...log.querySelectorAll(".csys")].map((e) => e.textContent),
				    times: log.querySelectorAll(".ctime").length, lines: log.querySelectorAll(".cline").length,
				    runNames: r15 && r17 && r15.parentElement === r17.parentElement ? r15.parentElement.querySelectorAll(".cname").length : -1,
				    quote: r15 ? (r15.querySelector(".cquote") || {}).textContent || "" : "", text: log.textContent,
				    bounds: { bad: bounds.bad, sideways: bounds.sideways },
				    atYouWhole: !!log.querySelector('[data-seq="21"] .cbub') && !log.querySelector('[data-seq="21"].fold') };
				})()`)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(v, &rep); err != nil {
					t.Fatal(err)
				}
				wantSeqs := "1,4,5,6,7,8,9,10,11,12,13,14,15,17,21"
				if got := strings.Join(rep.Seqs, ","); got != wantSeqs {
					t.Errorf("messages drawn: %s, want %s", got, wantSeqs)
				}
				for _, gone := range []string{"Do it on a branch", "Done: PR", "h-fx01", "每小时检查", "整理发布说明"} {
					if strings.Contains(rep.Text, gone) {
						t.Errorf("%q is drawn", gone)
					}
				}
				if !rep.AtYouWhole || !strings.Contains(rep.Text, "评审通过了，等你合并") {
					t.Errorf("an @you result is not an ordinary bubble: %+v", rep.AtYouWhole)
				}
				if len(rep.Sys) != 2 || rep.Sys[0] != "群已暂停" || rep.Sys[1] != "群已恢复" {
					t.Errorf("mad's lines: %v, want 群已暂停 and 群已恢复", rep.Sys)
				}
				if rep.Times != 2 {
					t.Errorf("%d time lines: the first message's, and one before the message 15 minutes on (the hidden ones in between add none)", rep.Times)
				}
				if rep.RunNames != 1 {
					t.Errorf("builder's messages 15 and 17, a nudge between them, are in %d runs (names), want one", rep.RunNames)
				}
				if !strings.Contains(rep.Quote, "architect") || !strings.Contains(rep.Quote, "Images in bus channels") {
					t.Errorf("the reply to the hidden handoff quotes %q", rep.Quote)
				}
				if rep.Lines != 2 {
					t.Errorf("%d folded lines, want the two long messages", rep.Lines)
				}
				for _, b := range rep.Bounds.Bad {
					t.Errorf("reaches past its column: %s", b)
				}
				for _, s := range rep.Bounds.Sideways {
					t.Errorf("scrolls sideways: %s", s)
				}
				shot(fmt.Sprintf("quiet-%d-%s", w, theme))

				// runs and time lines ignore a hidden message that comes between: out of the page's own drawing
				v, err = c.eval(`(() => {
				  const log = document.querySelector("#chanlog");
				  log.replaceChildren(); cv.run = null; cv.lastAt = 0;
				  const base = Date.now() - 3600e3;
				  const m = (seq, min, kind, from, text) => ({ seq, at: new Date(base + min * 60e3).toISOString(), from: { id: from, label: from }, kind, text });
				  drawMessage(m(101, 0, "text", "builder", "a"));
				  drawMessage(m(102, 11, "nudge", "mad", "n"));
				  drawMessage(m(103, 9, "text", "builder", "b"));
				  drawMessage(m(104, 40, "handoff", "architect", "h"));
				  drawMessage(m(105, 12, "text", "builder", "c"));
				  const trim = (seq, text) => drawMessage({ seq, at: new Date(base + 13 * 60e3).toISOString(), from: { id: "mad", label: "mad" }, kind: "system", text, event: "trimmed" });
				  trim(106, "Older messages were dropped; the last 500 are kept.");
				  trim(107, "Older messages were dropped; the last 400 are kept.");
				  drawMessage(m(108, 14, "text", "builder", "d"));
				  return { first: log.firstElementChild.className + ":" + log.firstElementChild.dataset.seq, notes: [...log.querySelectorAll(".csys")].map((e) => e.textContent), times: log.querySelectorAll(".ctime").length, groups: log.querySelectorAll(".cgrp").length, msgs: log.querySelectorAll(".cmsg").length };
				})()`)
				if err != nil {
					t.Fatal(err)
				}
				var synth struct {
					Times, Groups, Msgs int
					Notes               []string
					First               string
				}
				if err := json.Unmarshal(v, &synth); err != nil {
					t.Fatal(err)
				}
				// notes written before the event field existed are told by their text, and a trim note is first
				var old struct {
					Notes []string
					First string
				}
				v, err = c.eval(`(() => {
				  const log = document.querySelector("#chanlog");
				  log.replaceChildren(); cv.run = null; cv.lastAt = 0;
				  const sys = (seq, text, extra) => drawMessage({ seq, at: new Date().toISOString(), from: { id: "mad", label: "mad" }, kind: "system", text, ...extra });
				  drawMessage({ seq: 200, at: new Date().toISOString(), from: { id: "you", label: "you" }, kind: "text", text: "hi" });
				  sys(201, "Paused: 30 posts by agents with no word from a person. It waits for a person.");
				  sys(202, "Resumed by a person.");
				  sys(203, "Older messages were dropped; the last 500 are kept.");
				  sys(204, "Something else mad said");
				  sys(205, "Paused: h-1 lapsed", { handoff: { id: "h-1", to: "x", status: "lapsed" } });
				  return { notes: [...log.querySelectorAll(".csys")].map((e) => e.textContent), first: log.firstElementChild.className };
				})()`)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(v, &old); err != nil {
					t.Fatal(err)
				}
				if got := strings.Join(old.Notes, "|"); got != "更早的消息已清理|群已暂停|群已恢复|Something else mad said" || old.First != "csys trimmed" {
					t.Errorf("old notes read %q (first item %q), want 更早的消息已清理 first, then 群已暂停|群已恢复|Something else mad said", got, old.First)
				}
				if theme == "light" && w == 390 {
					shot("trim-note-390-light")
				}
				if synth.First != "csys trimmed:107" {
					t.Errorf("the first item is %q: the latest trim note must be the list's first item, above the oldest message and its time line", synth.First)
				}
				if len(synth.Notes) != 1 || synth.Notes[0] != "更早的消息已清理" {
					t.Errorf("the trim note reads %v, want 更早的消息已清理", synth.Notes)
				}
				if synth.Times != 1 || synth.Groups != 1 || synth.Msgs != 4 {
					t.Errorf("a hidden message came between builder's: %+v, want one time line, one run, three bubbles", synth)
				}
			})
		}
	}
	silentH, err := newHandler("silent", "")
	if err != nil {
		t.Fatal(err)
	}
	silentSrv := httptest.NewServer(silentH)
	defer silentSrv.Close()
	// both empty states are one quiet line: secondary colour, 14px, centred, about a third of the way down
	for _, c2 := range []struct{ name, base, want, shot string }{
		{"only hidden messages", quietSrv.URL, "agent 们在后台协作，有事会在这里 @ 你。", "quiet-empty-390-light"},
		{"nothing said yet", silentSrv.URL, "还没人说话。@ 一个 agent 可以叫醒它。", "silent-empty-390-light"},
	} {
		t.Run(c2.name, func(t *testing.T) {
			open(t, c2.base, 390, "light")
			var note struct {
				Text, Color      string
				Size, DX, Top, H float64
				Others           int
			}
			if !waitFor(20*time.Second, func() bool {
				v, err := c.eval(`document.querySelector("#chanlog .note") ? 1 : 0`)
				return err == nil && string(v) == "1"
			}) {
				t.Fatal("the empty channel says nothing")
			}
			time.Sleep(300 * time.Millisecond)
			v, err := c.eval(`(() => {
			  const n = document.querySelector("#chanlog .note"), s = document.querySelector("#chanscroll");
			  const nr = n.getBoundingClientRect(), sr = s.getBoundingClientRect(), cs = getComputedStyle(n);
			  return { text: n.textContent, color: cs.color, size: parseFloat(cs.fontSize), dx: (nr.left + nr.right) / 2 - (sr.left + sr.right) / 2,
			    top: nr.top - sr.top, h: sr.height, others: document.querySelectorAll("#chanlog > :not(.note)").length };
			})()`)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(v, &note); err != nil {
				t.Fatal(err)
			}
			if note.Text != c2.want {
				t.Errorf("the empty note is %q, want %q", note.Text, c2.want)
			}
			if note.Others != 0 {
				t.Errorf("something is drawn beside the note")
			}
			if note.Size != 14 || note.Color != "rgb(85, 85, 95)" {
				t.Errorf("the note is %vpx in %s, want 14px in the secondary colour (rgb(85, 85, 95))", note.Size, note.Color)
			}
			if note.DX < -3 || note.DX > 3 {
				t.Errorf("the note is %vpx off the centre", note.DX)
			}
			if frac := note.Top / note.H; frac < 0.2 || frac > 0.45 {
				t.Errorf("the note is %.0f%% of the way down the list area, want about a third", frac*100)
			}
			shot(c2.shot)
		})
	}
}
