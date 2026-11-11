package state

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
// lock. If the file changed since then, what others changed in it
// meanwhile is taken in first (see merge), so it isn't overwritten.
func (s *State) SaveMerged(base *State, since time.Time) error {
	unlock, err := lock()
	if err != nil {
		return err
	}
	defer unlock()
	if ModTime().After(since) {
		if disk, err := Load(); err == nil {
			s.merge(base, disk)
		}
	}
	return s.Save()
}

// merge takes into s what another writer did to disk since base, base
// being what s and disk last had in common. Where s left something as
// base had it, disk's version wins; where s changed it, s's does.
//
//   - Projects, agents and runs disk added are added (a project it added
//     back, though s ignored it, included), after s's own.
//   - Those disk removed are dropped, but a project s changed meanwhile
//     is kept, so what was done to it here isn't lost.
//   - Agents, runs and projects all three have are merged field by field,
//     as is State itself; the ignored list path by path.
func (s *State) merge(base, disk *State) {
	mergeFields(s, base, disk, "Projects", "Ignored")
	s.mergeIgnored(base, disk)
	for _, dp := range disk.Projects {
		sp := s.FindProject(dp.Path)
		if sp == nil {
			// Removed here, or ignored after the merge above: either s
			// removed it, or it still is removed.
			if base.FindProject(dp.Path) != nil || s.IsIgnored(dp.Path) {
				continue
			}
			s.Projects = append(s.Projects, dp)
			continue
		}
		if bp := base.FindProject(dp.Path); bp != nil {
			mergeFields(sp, bp, dp, "Agents", "Runs")
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
	kept := s.Projects[:0]
	for _, p := range s.Projects {
		bp := base.FindProject(p.Path)
		if bp == nil || disk.FindProject(p.Path) != nil {
			kept = append(kept, p)
			continue
		}
		if same(reflect.ValueOf(p), reflect.ValueOf(bp)) {
			continue // removed by another
		}
		// Removed by another, but changed here since (an agent added, a
		// rename): keep it, and lift the removal disk's ignored list
		// brought in, as adding it back by hand would.
		s.unignore(p.Path)
		kept = append(kept, p)
	}
	clear(s.Projects[len(kept):])
	s.Projects = kept
	// Fields are copied into s's own agents and runs, not swapped for
	// disk's, as the sidebar holds pointers to them across a save.
	for _, p := range s.Projects {
		for _, a := range p.Agents {
			_, b := base.FindAgent(a.ID)
			_, d := disk.FindAgent(a.ID)
			if b != nil && d != nil {
				mergeFields(a, b, d)
			}
		}
		for _, r := range p.Runs {
			_, b := base.FindRun(r.ID)
			_, d := disk.FindRun(r.ID)
			if b != nil && d != nil {
				mergeFields(r, b, d)
			}
		}
	}
}

// mergeIgnored merges the ignored lists path by path: a path s ignores as
// base did is ignored as disk has it. s's order is kept, disk's additions
// after.
func (s *State) mergeIgnored(base, disk *State) {
	var out []string
	for _, x := range s.Ignored {
		if base.IsIgnored(x) && !disk.IsIgnored(x) {
			continue // lifted by another
		}
		out = append(out, x)
	}
	for _, x := range disk.Ignored {
		if !s.IsIgnored(x) && !base.IsIgnored(x) {
			out = append(out, x) // added by another
		}
	}
	s.Ignored = out
}

// mergeFields sets each exported field of ours that is as base had it to
// theirs's, but those named in skip; ours, base and theirs point to
// structs of one type. A field ours changed keeps its value, whether or
// not theirs changed it too. Fields are walked, not listed, so one added
// to the type is merged without a change here.
func mergeFields(ours, base, theirs any, skip ...string) {
	o := reflect.ValueOf(ours).Elem()
	b := reflect.ValueOf(base).Elem()
	t := reflect.ValueOf(theirs).Elem()
	for i := range o.NumField() {
		f := o.Type().Field(i)
		if !f.IsExported() || slices.Contains(skip, f.Name) {
			continue
		}
		if same(o.Field(i), b.Field(i)) {
			o.Field(i).Set(t.Field(i))
		}
	}
}

var timeType = reflect.TypeOf(time.Time{})

// same is reflect.DeepEqual but for what doesn't survive a trip through
// the file: a nil slice is the same as an empty one, and times are
// compared as instants (no monotonic reading, no location).
func same(a, b reflect.Value) bool {
	if a.Type() == timeType {
		return a.Interface().(time.Time).Equal(b.Interface().(time.Time))
	}
	switch a.Kind() {
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return same(a.Elem(), b.Elem())
	case reflect.Slice:
		if a.Len() != b.Len() {
			return false
		}
		for i := range a.Len() {
			if !same(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for i := range a.NumField() {
			if !a.Type().Field(i).IsExported() {
				return reflect.DeepEqual(a.Interface(), b.Interface())
			}
			if !same(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a.Interface(), b.Interface())
}
