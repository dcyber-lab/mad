package transcript

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"time"

	"github.com/dcyber-lab/mad/internal/discover"
)

// relistEvery is how often Day looks for transcripts it doesn't follow
// yet (a new session); the ones it follows are read on every call.
const relistEvery = time.Minute

var usageKey = []byte(`"usage"`)

// Day adds up what today's responses cost at API list prices, over every
// transcript on the machine: the deck's agents, sessions in other
// terminals and the desktop app, subagents. A response counts on the
// local day it was logged. Like Reader, it follows files from where the
// last read stopped, and is not safe for concurrent use.
type Day struct {
	date   string // the local day the sum is for, 2006-01-02
	files  map[string]*dayFile
	cost   map[string]float64 // response id → cost; forks and resumes copy lines
	total  float64
	listed time.Time
	// written is replaceable in tests.
	written func(p discover.Provider, since time.Time) []string
}

type dayFile struct {
	p      discover.Provider
	offset int64 // end of the last complete line read
}

func NewDay() *Day {
	return &Day{written: discover.Provider.Written}
}

// Read brings the day's sum up to date and returns it, in USD. A new day
// starts over from nothing.
func (d *Day) Read(now time.Time) float64 {
	if date := now.Format(time.DateOnly); date != d.date {
		*d = Day{date: date, files: map[string]*dayFile{}, cost: map[string]float64{}, written: d.written}
	}
	if now.Sub(d.listed) >= relistEvery {
		d.listed = now
		y, m, day := now.Date()
		midnight := time.Date(y, m, day, 0, 0, 0, 0, now.Location())
		for _, p := range discover.Providers() {
			for _, path := range d.written(p, midnight) {
				if d.files[path] == nil {
					d.files[path] = &dayFile{p: p}
				}
			}
		}
	}
	for path, f := range d.files {
		d.update(path, f, now.Location())
	}
	return d.total
}

// update reads what was appended to path since the last call. A file that
// shrank was rewritten and is read again: responses are counted by id, so
// none twice.
func (d *Day) update(path string, f *dayFile, loc *time.Location) {
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	if fi.Size() < f.offset {
		f.offset = 0
	}
	if fi.Size() == f.offset {
		return
	}
	fh, err := os.Open(path)
	if err != nil {
		return
	}
	defer fh.Close()
	if _, err := fh.Seek(f.offset, io.SeekStart); err != nil {
		return
	}
	br := bufio.NewReaderSize(fh, 256<<10)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return // a partial last line waits for its newline
		}
		f.offset += int64(len(line))
		if !bytes.Contains(line, usageKey) {
			continue // most lines: no response to price
		}
		e, ok := f.p.Parse(line)
		if !ok || e.Usage == nil || e.At.In(loc).Format(time.DateOnly) != d.date {
			continue
		}
		if e.Message == "" {
			d.total += e.Usage.Cost
			continue
		}
		d.total += e.Usage.Cost - d.cost[e.Message]
		d.cost[e.Message] = e.Usage.Cost
	}
}
