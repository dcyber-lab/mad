package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/desktop"
	"github.com/dcyber-lab/mad/internal/desktop/api"
	"github.com/dcyber-lab/mad/internal/discover"
	"github.com/dcyber-lab/mad/internal/phone"
)

// The fixture answers in the server's own types: every JSON body it gives
// decodes into the type the real handler encodes, with no key left over.
// A failure here means the mock drifted from the server (or the type
// changed and the mock was not told).

// strict decodes body into v, refusing keys v has no field for.
func strict(body []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.More() {
		return fmt.Errorf("more after the value")
	}
	return nil
}

// call answers a request to the fixture at srv, checking the API version
// it is sent with.
func call(t *testing.T, srv *httptest.Server, method, path, body string) []byte {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: %d %s", method, path, r.StatusCode, b)
	}
	if got := r.Header.Get(api.VersionHeader); got != strconv.Itoa(api.Version) {
		t.Fatalf("%s %s: %s %q, want %d", method, path, api.VersionHeader, got, api.Version)
	}
	return b
}

// check decodes what path answers into v, strictly.
func check(t *testing.T, srv *httptest.Server, method, path, body string, v any) {
	t.Helper()
	if err := strict(call(t, srv, method, path, body), v); err != nil {
		t.Errorf("%s %s does not decode into %T: %v", method, path, v, err)
	}
}

func serveScenario(t *testing.T, name string) *httptest.Server {
	t.Helper()
	h, err := newHandler(name, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// Every scenario's deck, channels and transcripts are what the server
// sends.
func TestScenariosMatchTheServerTypes(t *testing.T) {
	if len(names()) == 0 {
		t.Fatal("no scenarios")
	}
	for _, name := range names() {
		t.Run(name, func(t *testing.T) {
			scn, err := load(name)
			if err != nil {
				t.Fatal(err)
			}
			srv := serveScenario(t, name)
			check(t, srv, "GET", "/api/deck", "", &desktop.Snapshot{})
			chans := map[string]bool{}
			for ch := range scn.Messages {
				chans[ch] = true
			}
			for ch := range scn.Receipts {
				chans[ch] = true
			}
			for ch := range chans {
				check(t, srv, "GET", "/api/bus/messages?channel="+url.QueryEscape(ch), "", &desktop.BusMessages{})
			}
			for id := range scn.Chat {
				check(t, srv, "GET", "/api/chat?id="+url.QueryEscape(id), "", &desktop.Chat{})
			}
		})
	}
}

// The other answers the fixture gives, and the deck after the acts that
// change it, are what the server sends.
func TestAnswersMatchTheServerTypes(t *testing.T) {
	srv := serveScenario(t, "channel-fold")
	check(t, srv, "POST", "/api/bus/post", `{"channel":"fold","text":"hi"}`, &bus.PostResult{})
	check(t, srv, "GET", "/api/bus/messages?channel=fold", "", &desktop.BusMessages{})
	check(t, srv, "POST", "/api/pair", `{"code":"`+strings.Repeat("a", 32)+`","name":"p","token":"`+strings.Repeat("0", 32)+`"}`, &api.Paired{})
	check(t, srv, "POST", "/api/budget", `{"op":"raise","usd":5}`, &bus.SpendState{})
	check(t, srv, "GET", "/api/phone", "", &phone.Status{})
	check(t, srv, "GET", "/api/push", "", &api.PushState{})
	check(t, srv, "GET", "/api/commands", "", &[]discover.Command{})
	check(t, srv, "GET", "/api/models", "", &[]discover.Model{})
	check(t, srv, "GET", "/api/phone/devices", "", &[]api.PhoneDevice{})
	check(t, srv, "GET", "/api/projects/suggest", "", &[]api.Suggestion{})
	check(t, srv, "GET", "/api/history", "", &[]api.Session{})
	check(t, srv, "GET", "/api/files", "", &[]api.FileEntry{})
	check(t, srv, "GET", "/api/deck", "", &desktop.Snapshot{})

	// An agent started, with a first message that goes and one that does
	// not, and its transcript after a moment.
	srv = serveScenario(t, "quiet")
	var started api.AgentAdded
	check(t, srv, "POST", "/api/agents", `{"project":"/fixture/mad","kind":"claude","text":"hello"}`, &started)
	check(t, srv, "POST", "/api/agents", `{"project":"/fixture/mad","kind":"claude","text":"[fail] hello"}`, &api.AgentAdded{})
	check(t, srv, "GET", "/api/deck", "", &desktop.Snapshot{})
	check(t, srv, "GET", "/api/chat?id="+started.ID, "", &desktop.Chat{})
	time.Sleep(startedTranscriptAfter + 100*time.Millisecond)
	check(t, srv, "GET", "/api/chat?id="+started.ID, "", &desktop.Chat{})

	// Both create requests allowed, an agent's from a channel and a
	// team's: the deck gets the agent, the team's channel and the
	// outcomes, and the channel the event line.
	srv = serveScenario(t, "create-confirm")
	var wg sync.WaitGroup
	for _, key := range []string{"create:c-00000001", "create:c-00000002"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := http.Post(srv.URL+"/api/inbox", "application/json", strings.NewReader(`{"key":"`+key+`","verb":"allow"}`))
			if err == nil {
				r.Body.Close()
			}
		}()
	}
	wg.Wait()
	check(t, srv, "GET", "/api/deck", "", &desktop.Snapshot{}) // creating
	time.Sleep(createCreatingFor + 100*time.Millisecond)
	check(t, srv, "GET", "/api/deck", "", &desktop.Snapshot{}) // created
	check(t, srv, "GET", "/api/bus/messages?channel=fold", "", &desktop.BusMessages{})
}
