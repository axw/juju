package logfwd

import "io"

type RecordSendCloser interface {
	io.Closer
	Send([]Record) error
}
