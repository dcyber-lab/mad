package desktop

import (
	"context"
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
