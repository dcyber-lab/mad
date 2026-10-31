package desktop

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/state"
)

func busPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 12, 9))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The person posts pictures to a channel, they are served with the token
// in a header, and what is no picture is refused.
func TestBusPostImages(t *testing.T) {
	isolate(t)
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: "aaaa1111-0000-0000-0000-000000000001", Kind: "claude", Name: "architect"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Create("pics", "", []string{"aaaa1111-0000-0000-0000-000000000001"}, bus.From{ID: bus.You, Label: bus.You}); err != nil {
		t.Fatal(err)
	}
	s := New(agent.Builtin())
	h := s.Handler()
	do := func(method, url, body string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, strings.NewReader(body))
		if auth {
			r.Header.Set("Authorization", "Bearer "+s.Token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	png64 := base64.StdEncoding.EncodeToString(busPNG(t))
	post := func(images string) *httptest.ResponseRecorder {
		return do("POST", "/api/bus/post", `{"channel":"pics","text":"look","images":`+images+`}`, true)
	}

	if w := post(`[{"mime":"image/png","data":"` + png64 + `"}]`); w.Code != 200 {
		t.Fatalf("post: %d %s", w.Code, w.Body)
	}
	// Pictures alone.
	if w := do("POST", "/api/bus/post", `{"channel":"pics","text":"","images":[{"mime":"image/png","data":"`+png64+`"}]}`, true); w.Code != 200 {
		t.Errorf("pictures alone: %d %s", w.Code, w.Body)
	}
	w := do("GET", "/api/bus/messages?channel=pics", "", true)
	var got struct {
		Messages []struct {
			Images []struct {
				ID, Mime string
				W, H     int
			}
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got.Messages) < 1 || len(got.Messages[0].Images) != 1 {
		t.Fatalf("messages: %s", w.Body)
	}
	im := got.Messages[0].Images[0]
	if im.Mime != "image/png" || im.W != 12 || im.H != 9 {
		t.Errorf("image %+v", im)
	}

	// Served to a token, not to anyone; an id that names no file is 404.
	if w := do("GET", "/api/bus/image?id="+im.ID, "", true); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), busPNG(t)) {
		t.Errorf("image: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if w := do("GET", "/api/bus/image?id="+im.ID, "", false); w.Code != http.StatusForbidden {
		t.Errorf("image without a token: %d", w.Code)
	}
	if w := do("GET", "/api/bus/image?id=../../etc/passwd", "", true); w.Code != http.StatusNotFound {
		t.Errorf("a path as an id: %d", w.Code)
	}

	// Refused: not base64, not a picture, a script called a png, too many.
	text64 := base64.StdEncoding.EncodeToString([]byte("<script>alert(1)</script>"))
	for name, images := range map[string]string{
		"not base64":    `[{"mime":"image/png","data":"@@@"}]`,
		"not a picture": `[{"mime":"image/png","data":"` + text64 + `"}]`,
		"too many":      "[" + strings.Repeat(`{"mime":"image/png","data":"`+png64+`"},`, bus.MaxImages) + `{"mime":"image/png","data":"` + png64 + `"}]`,
	} {
		if w := post(images); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
}

func imageFiles(t *testing.T) int {
	t.Helper()
	files, _ := os.ReadDir(filepath.Join(paths.StateDir(), "bus", "images"))
	return len(files)
}

// What can't be posted keeps nothing, and limits are met before anything is
// decoded: seven pictures, or one claimed too big, are refused as such, not
// as the base64 garbage they are here.
func TestBusPostImagesLimitsAndTarget(t *testing.T) {
	isolate(t)
	const aid = "aaaa1111-0000-0000-0000-000000000001"
	const other = "bbbb2222-0000-0000-0000-000000000002"
	st := &state.State{}
	p, _ := st.AddProject("/code/a")
	p.Agents = []*state.Agent{{ID: aid, Kind: "claude", Name: "architect"}, {ID: other, Kind: "claude", Name: "builder"}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := bus.Create("here", "", []string{aid}, bus.From{ID: bus.You, Label: bus.You}); err != nil {
		t.Fatal(err)
	}
	s := New(agent.Builtin())
	h := s.Handler()
	png64 := base64.StdEncoding.EncodeToString(busPNG(t))
	do := func(url, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", url, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+s.Token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	one := `{"mime":"image/png","data":"` + png64 + `"}`

	// A channel that is not there, a DM of two agents, an agent that is not
	// there: refused, and not a file kept.
	for name, c := range map[string]struct{ url, body string }{
		"missing channel": {"/api/bus/post", `{"channel":"nowhere","text":"hi","images":[` + one + `]}`},
		"their DM":        {"/api/bus/post", `{"channel":"` + bus.DMChannel(aid, other) + `","text":"hi","images":[` + one + `]}`},
		"no such agent":   {"/api/bus/dm", `{"to":"cccc3333-0000-0000-0000-000000000009","text":"hi","images":[` + one + `]}`},
	} {
		if w := do(c.url, c.body); w.Code < 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
		if n := imageFiles(t); n != 0 {
			t.Fatalf("%s kept %d files", name, n)
		}
	}
	if w := do("/api/bus/post", `{"channel":"`+bus.DMChannel(aid, other)+`","text":"hi","images":[`+one+`]}`); w.Code != http.StatusForbidden {
		t.Errorf("their DM: %d", w.Code)
	}

	// Seven: refused by count, before any is decoded (this base64 is junk).
	junk := `{"mime":"image/png","data":"@@@@"}`
	seven := "[" + strings.Repeat(junk+",", bus.MaxImages) + junk + "]"
	w := do("/api/bus/post", `{"channel":"here","text":"hi","images":`+seven+`}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "at most 6") {
		t.Errorf("seven: %d %s", w.Code, w.Body)
	}
	// One claimed over the cap: refused by its length, before it is decoded.
	big := `{"mime":"image/png","data":"` + strings.Repeat("@", (bus.MaxImageBytes/3+2)*4) + `"}`
	w = do("/api/bus/post", `{"channel":"here","text":"hi","images":[`+one+`,`+big+`]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "image 2 is over") {
		t.Errorf("oversize: %d %s", w.Code, w.Body)
	}
	if n := imageFiles(t); n != 0 {
		t.Errorf("a refused post kept %d files, the good one among them", n)
	}
	// A good one still goes.
	if w := do("/api/bus/post", `{"channel":"here","text":"hi","images":[`+one+`]}`); w.Code != 200 {
		t.Errorf("a good post: %d %s", w.Code, w.Body)
	}
	if n := imageFiles(t); n != 1 {
		t.Errorf("%d files for one picture", n)
	}
}

// The page's limit is the server's.
func TestPageImageLimitsMatchServer(t *testing.T) {
	b, err := webFS.ReadFile("web/actions.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const MAX_IMAGES = (\d+);`).FindSubmatch(b)
	n := regexp.MustCompile(`const MAX_IMAGE_BYTES = (\d+) << 20;`).FindSubmatch(b)
	if m == nil || n == nil {
		t.Fatal("the page's limits are not where this looks")
	}
	if string(m[1]) != strconv.Itoa(bus.MaxImages) || string(n[1]) != strconv.Itoa(bus.MaxImageBytes>>20) {
		t.Errorf("page: %s images of %s MB; server: %d of %d MB", m[1], n[1], bus.MaxImages, bus.MaxImageBytes>>20)
	}
}
