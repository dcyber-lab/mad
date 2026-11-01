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
