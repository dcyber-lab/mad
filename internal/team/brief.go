package team

import (
	"fmt"
	"strings"

	"github.com/dcyber-lab/mad/internal/drive"
	"github.com/dcyber-lab/mad/internal/git"
	"github.com/dcyber-lab/mad/internal/paths"
	"github.com/dcyber-lab/mad/internal/run"
	"github.com/dcyber-lab/mad/internal/skill"
	"github.com/dcyber-lab/mad/internal/state"
)

// Brief tells agent id its part in its team, for its MCP server's
// instructions: who it is there, the channel, the lead, the teammates
// with the first line of their briefs, and its own. It is "" for an agent
// that is in no team.
func Brief(st *state.State, id string) string {
	_, me := st.FindAgent(id)
	if me == nil || me.Team == "" {
		return ""
	}
	var f file
	if paths.ReadJSON(recordsFile(), &f) != nil {
		return ""
	}
	for _, rec := range f.Teams {
		if rec.Name == me.Team {
			return rec.brief(st, me.Role)
		}
	}
	return ""
}

func (rec Record) brief(st *state.State, role string) string {
	// Who a role is on the bus: its agent's label.
	label := func(x RoleRec) string {
		if _, a := st.FindAgent(x.Agent); a != nil && x.Agent != "" {
			return drive.Label(a)
		}
		return x.Role
	}
	var me *RoleRec
	for i := range rec.Roles {
		if rec.Roles[i].Role == role {
			me = &rec.Roles[i]
		}
	}
	if me == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are the %s of team %s, working in #%s on the person's goal.", role, rec.Name, rec.Channel)
	if lead, ok := rec.Lead(); ok {
		if lead.Role == role {
			b.WriteString(" You are the lead: the channel's work answers to you.")
		} else {
			fmt.Fprintf(&b, " The lead is %s (@%s).", lead.Role, label(lead))
		}
	}
	var mates []string
	for _, x := range rec.Roles {
		if x.Role == role {
			continue
		}
		line := fmt.Sprintf("%s (@%s, %s): %s", x.Role, label(x), x.describe(), firstLine(x.Brief))
		mates = append(mates, strings.TrimRight(strings.TrimSpace(line), ":"))
	}
	if len(mates) > 0 {
		b.WriteString("\nTeammates: " + strings.Join(mates, "; ") + ".")
	}
	if me.Brief != "" {
		b.WriteString("\nYour part: " + me.Brief)
	}
	if me.Skill != "" {
		kind := run.ParseChoice(me.Spec).Kind
		if path := skill.Find(kind, rec.dir(), me.Skill); path != "" {
			fmt.Fprintf(&b, "\nWork by the skill %s: read %s in full before your first task, and follow it. Where the skill and your part here differ, your part wins: hand work over on the bus, and push nothing and open no pull request unless the lead or the person asks for it.", me.Skill, path)
		} else {
			fmt.Fprintf(&b, "\nYour part names the skill %s, which is not on this machine any more: work without it.", me.Skill)
		}
	}
	b.WriteString("\n")
	return b.String()
}

// dir is where the team's agents work: its branch's worktree, or the
// project.
func (rec Record) dir() string {
	if rec.Branch != "" {
		return git.WorktreeDir(rec.Project, rec.Branch)
	}
	return rec.Project
}

// describe says what plays a role: claude opus, codex (read only).
func (x RoleRec) describe() string {
	s := strings.NewReplacer(":", " ", "@", " at ").Replace(x.Spec)
	if x.ReadOnly {
		s += ", read only"
	}
	return s
}

// firstLine is the first line of s, short.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	return s
}
