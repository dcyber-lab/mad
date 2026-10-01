package state

import (
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// lock holds the state lock until the returned func is called: writers
// in different processes (runners, `mad add`, the sidebar) take it around
// reading and writing the file, so none writes from a copy another
// already replaced.
func lock() (func(), error) {
	path := paths.StateFile() + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Update loads the state under the lock, lets fn change it, and saves it
// unless fn fails. Changes to the state go through here, not Load and Save.
func Update(fn func(*State) error) error {
	unlock, err := lock()
	if err != nil {
		return err
	}
	defer unlock()
	s, err := Load()
	if err != nil {
		return err
	}
	if err := fn(s); err != nil {
		return err
	}
	return s.Save()
}

// SaveMerged saves s, which was loaded (or last saved) as base, under the
// lock. If the file changed since then, what others added to it meanwhile
// (projects, agents, runs) is taken in first, so it isn't overwritten.
func (s *State) SaveMerged(base *State, since time.Time) error {
	unlock, err := lock()
	if err != nil {
		return err
	}
	defer unlock()
	if ModTime().After(since) {
		if disk, err := Load(); err == nil {
			s.mergeAdded(base, disk)
		}
	}
	return s.Save()
}

// mergeAdded takes in what another writer did to disk since base: the
// projects, agents and runs it added (a project it added back, though s
// ignored it, included), and drops the agents and runs it removed.
func (s *State) mergeAdded(base, disk *State) {
	for _, dp := range disk.Projects {
		sp := s.FindProject(dp.Path)
		if sp == nil {
			if base.FindProject(dp.Path) != nil {
				continue // removed here
			}
			if s.IsIgnored(dp.Path) {
				if !base.IsIgnored(dp.Path) || disk.IsIgnored(dp.Path) {
					continue // removed here, or still removed
				}
				s.unignore(dp.Path) // added back by another
			}
			s.Projects = append(s.Projects, dp)
			continue
		}
		for _, da := range dp.Agents {
			if _, a := s.FindAgent(da.ID); a != nil {
				continue
			}
			if _, a := base.FindAgent(da.ID); a == nil {
				sp.Agents = append(sp.Agents, da)
			}
		}
		for _, dr := range dp.Runs {
			if _, r := s.FindRun(dr.ID); r != nil {
				continue
			}
			if _, r := base.FindRun(dr.ID); r == nil {
				sp.Runs = append(sp.Runs, dr)
			}
		}
		// Removed by another: in base, gone from disk.
		for _, a := range append([]*Agent(nil), sp.Agents...) {
			if _, b := base.FindAgent(a.ID); b != nil {
				if _, d := disk.FindAgent(a.ID); d == nil {
					s.RemoveAgent(a.ID)
				}
			}
		}
		for _, r := range append([]*Run(nil), sp.Runs...) {
			if _, b := base.FindRun(r.ID); b != nil {
				if _, d := disk.FindRun(r.ID); d == nil {
					s.RemoveRun(r.ID)
				}
			}
		}
	}
}
