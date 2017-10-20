// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package globalclock

import (
	"time"

	"gopkg.in/mgo.v2/bson"
)

// clockDocID is the document ID for the global clock document.
const clockDocID = "g"

// clockDoc contains the current global virtual time.
type clockDoc struct {
	DocID string `bson:"_id"`
	Time  int64  `bson:"time"`
}

func newClockDoc(t time.Time) *clockDoc {
	return &clockDoc{
		DocID: clockDocID,
		Time:  t.UnixNano(),
	}
}

func (d clockDoc) time() time.Time {
	return time.Unix(0, d.Time)
}

func setTimeDoc(t time.Time) bson.D {
	return bson.D{{"$set", bson.D{{"time", t.UnixNano()}}}}
}
