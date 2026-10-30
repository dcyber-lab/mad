package desktop

import (
	"net/http"
	"path/filepath"

	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/state"
)

// The Skills page: what agents saved for the person to approve, and the
// skills approved already.

type pendingSkill struct {
	skill.Pending
	ProjectName string `json:"project_name,omitempty"`
	Text        string `json:"text"` // its SKILL.md, to read and edit
}

type approvedSkill struct {
	skill.Skill
	ProjectName string `json:"project_name,omitempty"`
}

// skills answers with the pending skills and the approved ones: of the
// project given (a path or name) with the person's own, else of every
// project in the deck.
func (s *Server) skills(w http.ResponseWriter, r *http.Request) {
	st, err := state.Load()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	pending, err := skill.PendingList()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	names := map[string]string{}
	var all []string
	for _, p := range st.Projects {
		names[p.Path] = p.Name
		all = append(all, p.Path)
	}
	name := func(path string) string {
		if n, ok := names[path]; ok {
			return n
		}
		return filepath.Base(path)
	}
	out := struct {
		Pending  []pendingSkill  `json:"pending"`
		Approved []approvedSkill `json:"approved"`
	}{Pending: []pendingSkill{}, Approved: []approvedSkill{}}
	for _, p := range pending {
		pn := ""
		if p.Project != "" {
			pn = name(p.Project)
		}
		out.Pending = append(out.Pending, pendingSkill{p, pn, skill.Edit(p.Name, p.Description, p.Body)})
	}
	add := func(list []skill.Skill) {
		for _, k := range list {
			pn := ""
			if k.Project != "" {
				pn = name(k.Project)
			}
			out.Approved = append(out.Approved, approvedSkill{k, pn})
		}
	}
	if q := r.URL.Query().Get("project"); q != "" {
		for _, p := range st.Projects {
			if p.Name == q || p.Path == q {
				q = p.Path
			}
		}
		add(skill.List(q))
	} else {
		add(skill.List(""))
		for _, p := range all {
			for _, k := range skill.List(p) {
				if k.Scope == skill.Project {
					add([]skill.Skill{k})
				}
			}
		}
	}
	reply(w, out)
}

func (s *Server) skillApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string  `json:"name"`
		Body *string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := skill.Approve(req.Name, req.Body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	changed()
	reply(w, map[string]bool{"ok": true})
}

func (s *Server) skillDiscard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := skill.Discard(req.Name); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	changed()
	reply(w, map[string]bool{"ok": true})
}
