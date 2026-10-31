package desktop

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dcyber-lab/mad/internal/agent"
	"github.com/dcyber-lab/mad/internal/bus"
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
