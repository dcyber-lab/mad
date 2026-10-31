package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/dcyber-lab/mad/internal/phone"
)

// The window offers to put itself on the phone (package phone): GET says
// how it stands, POST serves it, DELETE stops it; /api/phone/qr is the QR
// code of its address.

// phoneGet, phoneOn and phoneOff are package phone's, replaced in tests.
var (
	phoneGet = phone.Get
	phoneOn  = phone.On
	phoneOff = phone.Off
)

func (s *Server) phone(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var st phone.Status
	switch r.Method {
	case http.MethodPost:
		var err error
		if st, err = phoneOn(ctx); err != nil && st.Problem == "" {
			st.Problem = err.Error()
		}
	case http.MethodDelete:
		if err := phoneOff(ctx); err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		st = phoneGet(ctx)
	default:
		st = phoneGet(ctx)
	}
	reply(w, st)
}

func (s *Server) phoneQR(w http.ResponseWriter, r *http.Request) {
	st := phoneGet(r.Context())
	if st.URL == "" {
		http.Error(w, "not served to the phone", http.StatusNotFound)
		return
	}
	png, err := phone.PNG(st.URL)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

// pair trades a pairing code for a device's token. It has no token to
// ask for: the code, good once and for minutes, is the credential.
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code, Name string }
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	token, id, err := phone.Pair(in.Code, in.Name)
	if errors.Is(err, phone.ErrBadCode) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, map[string]string{"token": token, "id": id})
}

// phoneDevices lists the paired phones (GET) or removes one (DELETE ?id=).
func (s *Server) phoneDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		if _, err := phone.Revoke(r.URL.Query().Get("id")); err != nil {
			fail(w, http.StatusNotFound, err)
			return
		}
	}
	type dev struct {
		ID       string    `json:"id"`
		Name     string    `json:"name"`
		Created  time.Time `json:"created"`
		LastSeen time.Time `json:"last_seen"`
	}
	out := []dev{}
	for _, d := range phone.Devices() {
		out = append(out, dev{d.ID, d.Name, d.Created, d.LastSeen})
	}
	reply(w, out)
}
