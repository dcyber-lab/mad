package host

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/dcyber-lab/mad/internal/paths"
)

// Dial connects to agent id's host, if it runs.
func Dial(id string) (net.Conn, error) {
	return net.DialTimeout("unix", paths.HostSocket(id), time.Second)
}

// Peek returns agent id's state, as its host has it.
func Peek(id string) (State, error) {
	conn, err := Dial(id)
	if err != nil {
		return State{}, err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for sc.Scan() {
		var l Line
		if json.Unmarshal(sc.Bytes(), &l) == nil && l.Type == "state" && l.State != nil {
			return *l.State, nil
		}
	}
	return State{}, errors.New("the host said nothing")
}

// Do has agent id's host carry out c, and returns how that went.
func Do(id string, c Command) error {
	conn, err := Dial(id)
	if err != nil {
		return err
	}
	defer conn.Close()
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for sc.Scan() {
		var l Line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		switch l.Type {
		case "done":
			return nil
		case "error":
			return errors.New(l.Error)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("the host went away")
}
