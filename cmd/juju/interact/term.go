package interact

import (
	"io"
	"os"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/juju/errors"
)

// ErrNoTerminal is returned by Terminal when the process
// is not running in a terminal.
var ErrNoTerminal = errors.New("terminal not available")

// ReadLineWriteCloser is an interface that satisfies
// io.WriteCloser, but also supports reading lines at
// a time.
type ReadLineWriteCloser interface {
	io.WriteCloser
	LineReader
}

// LineReader is an interface providing a ReadLine method,
// satisfied by *terminal.Terminal.
type LineReader interface {
	ReadLine() (string, error)
}

// Terminal returns a ReadLineWriteCloser for the standard
// I/O terminal. The ReadLineWriteCloser must be closed
// to restore the terminal mode to its previous state.
func NewTerminal() (ReadLineWriteCloser, error) {
	fd := int(os.Stdin.Fd())
	if !terminal.IsTerminal(fd) {
		return nil, ErrNoTerminal
	}
	oldState, err := terminal.MakeRaw(fd)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return &term{
		terminal.NewTerminal(os.Stdin, ""),
		fd, oldState,
	}, nil
}

type term struct {
	*terminal.Terminal
	fd       int
	oldState *terminal.State
}

// Close is part of the ReadLineWriteCloser interface.
func (t *term) Close() error {
	return terminal.Restore(t.fd, t.oldState)
}
