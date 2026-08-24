// Copyright (c) 2020 Dean Jackson <deanishe@deanishe.net>
// Modifications Copyright (c) 2026 Andres Mena Godino
// MIT Licence applies http://opensource.org/licenses/MIT

package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

const timeout = time.Second * 5

// errTimeout is returned by firefox.call if execution time exceeds timeout.
type errTimeout struct {
	ID string // command ID
}

func (err errTimeout) Error() string {
	return fmt.Sprintf("timeout: %q", err.ID)
}

// command is a command sent to the extension.
type command struct {
	ID     string      `json:"id"`
	Name   string      `json:"command"`
	Params interface{} `json:"params"`
	ch     chan response
}

func (c command) String() string {
	return fmt.Sprintf("command #%s - %q", c.ID, c.Name)
}

// encode command into extension STDIO format.
func (c command) encode() ([]byte, error) {
	js, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(len(js)))
	b = append(b, js...)
	return b, nil
}

// response is a generic response from the browser extension.
// The full JSON response from the extension is contained in data to
// be unmarshalled by the receiver.
type response struct {
	ID   string `json:"id"`    // ID of the command this is a response to
	Err  string `json:"error"` // error message returned by extension
	data []byte // full JSON response
	err  error  // error encountered decoding response
}

func (r response) String() string {
	return fmt.Sprintf("response #%s - %d bytes", r.ID, len(r.data))
}

// firefox communicates with the browser extension via STDIN/STOUT.
type firefox struct {
	commands  chan command
	responses chan response
	done      chan struct{}
	closed    chan struct{} // closed when the extension disconnects (STDIN EOF)
	closeOnce sync.Once
	handlers  map[string]chan response
}

func newFirefox() *firefox {
	return &firefox{
		commands:  make(chan command, 1),
		responses: make(chan response, 1),
		done:      make(chan struct{}),
		closed:    make(chan struct{}),
		handlers:  map[string]chan response{},
	}
}

// Closed returns a channel that is closed when the connection to the browser
// extension is lost, i.e. its STDIN reaches EOF because the browser quit (or
// was force-quit). The server uses this to shut down promptly instead of
// lingering as an orphan that holds the RPC socket.
func (f *firefox) Closed() <-chan struct{} { return f.closed }

// markClosed signals (exactly once) that the extension has disconnected.
func (f *firefox) markClosed() { f.closeOnce.Do(func() { close(f.closed) }) }

// run the read/write loop to send & receive messages from the extension.
func (f *firefox) run() {
	go func() {
		// However the reader loop ends (EOF when the browser quits, or a read
		// error), tell the server the extension has disconnected so it can shut
		// down and release the RPC socket.
		defer f.markClosed()
		b := make([]byte, 4)
		for {
			// read payload size
			_, err := os.Stdin.Read(b)
			if err == io.EOF {
				return
			}
			if err != nil {
				log.Printf("[ERROR] read from stdin: %v", err)
				return
			}

			// read payload
			data := make([]byte, binary.LittleEndian.Uint32(b))
			_, err = io.ReadFull(os.Stdin, data)
			if err == io.EOF {
				return
			}
			if err != nil {
				log.Printf("[ERROR] read from stdin: %v", err)
				return
			}
			// log.Printf("received %s", data)
			var r response
			if err := json.Unmarshal(data, &r); err != nil {
				r.err = err
			}
			r.data = data
			log.Printf("received %v", r)
			f.responses <- r
		}
	}()

	for {
		select {
		case <-f.done:
			return

		case cmd := <-f.commands:
			var (
				data []byte
				// n    int
				err error
			)
			if data, err = cmd.encode(); err != nil {
				cmd.ch <- response{err: err}
				break
			}
			if _, err = os.Stdout.Write(data); err != nil {
				cmd.ch <- response{err: err}
				break
			}
			log.Printf("sent %v", cmd)
			f.handlers[cmd.ID] = cmd.ch

		case r := <-f.responses:
			ch, ok := f.handlers[r.ID]
			if ok {
				ch <- r
				delete(f.handlers, r.ID)
			} else {
				log.Printf("[ERROR] no handler for message %q", r.ID)
			}
		}
	}
}

// call passes a command to the extension and unmarshals the response into pointer v.
// It returns an error if the command fails, the response isn't understood or
// the respones takes too long.
func (f *firefox) call(cmd string, params, v interface{}) error {
	c := command{
		ID:     newID(),
		Name:   cmd,
		Params: params,
		ch:     make(chan response),
	}
	f.commands <- c

	t := time.Tick(timeout)
	select {
	case r := <-c.ch:
		if r.err != nil {
			return r.err
		}
		return json.Unmarshal(r.data, v)
	case <-t:
		return errTimeout{c.ID}
	}
}

// exist the run loop.
func (f *firefox) stop() { close(f.done) }

var lastUID = 0

// create a new command ID.
func newID() string {
	lastUID++
	return fmt.Sprintf("%d.%d", time.Now().Unix(), lastUID)
}
