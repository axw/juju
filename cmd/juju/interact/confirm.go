package interact

import (
	"strings"

	"github.com/juju/errors"
)

var ErrUnconfirmed = errors.New(`user aborted the operation`)

// Confirm reads a line from the given line reader, and
// returns an error if we do not read "y" or "yes" from
// user input.
func Confirm(r LineReader) error {
	line, err := r.ReadLine()
	if err != nil {
		return errors.Trace(err)
	}
	answer := strings.ToLower(line)
	if answer != "y" && answer != "yes" {
		return ErrUnconfirmed
	}
	return nil
}
