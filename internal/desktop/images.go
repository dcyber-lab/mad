package desktop

import (
	"bufio"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dcyber-lab/mad/internal/state"
)

// Images in a conversation: those its transcript holds (a screenshot a
// call gave back, one the user sent), fetched by where they are in it
// (GET /api/image), and image files an agent's reply names (GET
// /api/file).

// imageTypes are the images served, by extension. Not SVG: it can hold
// a script.
var imageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
}

func imageType(mime string) bool {
	for _, t := range imageTypes {
		if t == mime {
			return true
		}
	}
	return false
}

// maxImageFile bounds an image file served.
const maxImageFile = 25 << 20

// image answers GET /api/image?id=AGENT&file=KEY&at=OFFSET&e=ENTRY&n=IMAGE:
// image n of entry e of the line at offset at of the agent's transcript,
// as /api/chat named it.
func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, a, ok := findAgent(w, q.Get("id"))
	if !ok {
		return
	}
	conv, path, key := transcriptOf(r.Context(), p, a)
	if path == "" || key != q.Get("file") {
		fail(w, http.StatusNotFound, errors.New("no such transcript"))
		return
	}
	at, err1 := strconv.ParseInt(q.Get("at"), 10, 64)
	e, err2 := strconv.Atoi(q.Get("e"))
	n, err3 := strconv.Atoi(q.Get("n"))
	if err := errors.Join(err1, err2, err3); err != nil || at < 0 {
		fail(w, http.StatusBadRequest, errors.New("bad image address"))
		return
	}
	f, err := os.Open(path)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	defer f.Close()
	if _, err := f.Seek(at, io.SeekStart); err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	line, err := bufio.NewReaderSize(f, 1<<20).ReadBytes('\n')
	if err != nil {
		fail(w, http.StatusNotFound, errors.New("no such line"))
		return
	}
	entries := conv.Chat(line)
	if e < 0 || e >= len(entries) || n < 0 || n >= len(entries[e].Images) {
		fail(w, http.StatusNotFound, errors.New("no such image"))
		return
	}
	img := entries[e].Images[n]
	data, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil || !imageType(img.Mime) {
		fail(w, http.StatusUnsupportedMediaType, errors.New("not an image shown here"))
		return
	}
	// A line of a transcript stays as it was.
	serveImage(w, img.Mime, data, "private, max-age=86400")
}

// file answers GET /api/file?id=AGENT&path=PATH: an image file a reply of
// the agent's names, PATH from its directory, from home (~/), or whole.
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, a, ok := findAgent(w, q.Get("id"))
	if !ok {
		return
	}
	path := q.Get("path")
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, rest)
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(p.Dir(a), path)
	}
	mime := imageTypes[strings.ToLower(filepath.Ext(path))]
	if mime == "" {
		fail(w, http.StatusUnsupportedMediaType, errors.New("not an image shown here"))
		return
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		fail(w, http.StatusNotFound, errors.New("no such image"))
		return
	}
	if fi.Size() > maxImageFile {
		fail(w, http.StatusRequestEntityTooLarge, errors.New("too big to show"))
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	serveImage(w, mime, data, "no-cache")
}

func serveImage(w http.ResponseWriter, mime string, data []byte, cache string) {
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", cache)
	_, _ = w.Write(data)
}

// findAgent finds agent id, answering 404 (or 500) when it can't.
func findAgent(w http.ResponseWriter, id string) (*state.Project, *state.Agent, bool) {
	st, err := state.Load()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return nil, nil, false
	}
	p, a := st.FindAgent(id)
	if a == nil {
		fail(w, http.StatusNotFound, errors.New("no such agent"))
		return nil, nil, false
	}
	return p, a, true
}
