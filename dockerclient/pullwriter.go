// Copyright 2016 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dockerclient

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"runtime"

	"github.com/cheggaaa/pb"
)

// partialBuffer defines a buffer for holding partial JSON responses.
type partialBuffer []byte

func (pb partialBuffer) hasContents() bool { return len(pb) != 0 }

func (pb *partialBuffer) set(in []byte) { *pb = in }

func (pb *partialBuffer) getAndEmpty(in []byte) (ret []byte) {
	ret = append(ret, *pb...)
	ret = append(ret, in...)

	*pb = []byte{}
	return
}

// dockerResponse defines the expected data from Docker pull logs.
type dockerResponse struct {
	Error          string         `json:"error,omitempty"`
	Stream         string         `json:"stream,omitempty"`
	Status         string         `json:"status,omitempty"`
	ID             string         `json:"id,omitempty"`
	ProgressDetail progressDetail `json:"progressDetail,omitempty"`
}

// progressDetail defines progress details expected for a Docker pull.
type progressDetail struct {
	Current int `json:"current,omitempty"`
	Total   int `json:"total,omitempty"`
}

// pullProgressDisplay is a writer which consumes the JSON form of Docker pull logs and displays
// a nice set of progress bars.
type pullProgressDisplay struct {
	partialBuffer    *partialBuffer
	hasPartialBuffer bool
	pbMap            map[string]*pb.ProgressBar
	pbCounter        int
	bars             []*pb.ProgressBar
	pool             *pb.Pool
	hasProgressBars  bool
	tagName          string
}

// newPullProgressDisplay creates a new pull progress display.
func newPullProgressDisplay(tagName string, layerCount int) *pullProgressDisplay {
	var bars = make([]*pb.ProgressBar, 0, layerCount)
	for i := 0; i < layerCount; i++ {
		progressBar := pb.New(100).Postfix(" Initializing")
		progressBar.SetMaxWidth(80)
		progressBar.ShowCounters = false
		progressBar.AlwaysUpdate = true
		bars = append(bars, progressBar)
	}

	// Create a pool of progress bars.
	pool, err := pb.StartPool(bars...)

	return &pullProgressDisplay{
		tagName:          tagName,
		partialBuffer:    &partialBuffer{},
		hasPartialBuffer: false,
		bars:             bars,
		pbMap:            map[string]*pb.ProgressBar{},
		pbCounter:        0,
		pool:             pool,
		hasProgressBars:  err == nil,
	}
}

func (w *pullProgressDisplay) Done() {
	if w.hasProgressBars {
		w.pool.Stop()
	}
}

func (w *pullProgressDisplay) updateStatus(m dockerResponse) {
	if m.ID == "" || m.ID == w.tagName {
		return
	}

	if !w.hasProgressBars {
		if m.ProgressDetail.Total == 0 {
			log.Printf("%v: %v\n", m.ID, m.Status)
		}
		return
	}

	if _, found := w.pbMap[m.ID]; !found {
		if w.pbCounter >= len(w.bars) {
			return
		}

		w.pbMap[m.ID] = w.bars[w.pbCounter]
		w.pbMap[m.ID].Prefix(m.ID + " ")
		w.pbCounter++
	}

	if m.ProgressDetail.Total > 0 {
		current := int((float64(m.ProgressDetail.Current) / float64(m.ProgressDetail.Total)) * 100)
		w.pbMap[m.ID].Set(current)
	} else {
		w.pbMap[m.ID].Set(100)
	}

	w.pbMap[m.ID].Postfix(" " + m.Status)
}

func (w *pullProgressDisplay) Write(p []byte) (n int, err error) {
	originalLength := len(p)

	// Note: Sometimes Docker returns to us only the beginning of a stream,
	// so we have to prepend any existing data from the previous call.
	if w.partialBuffer.hasContents() {
		p = w.partialBuffer.getAndEmpty(p)
	}

	buf := bytes.NewBuffer(p)
	dec := json.NewDecoder(buf)

	for {
		// Yield to the Go scheduler. Sometimes, when we have very large number of
		// messages, we need to yield to ensure that other goroutines are not
		// starved (specifically the heartbeat).
		runtime.Gosched()

		// Attempt to decode what was written into a Docker Reponse.
		var m dockerResponse
		if err = dec.Decode(&m); err == io.EOF {
			break
		} else if err == io.ErrUnexpectedEOF {
			// If we get an unexpected EOF, it means that the JSON response from
			// Docker was too large to fit into the single Write call. Therefore, we
			// store any unparsed data and prepend it on the next call.
			var bufferedData []byte
			bufferedData, err = ioutil.ReadAll(dec.Buffered())
			if err != nil {
				log.Fatalf("Error when reading buffered logs: %v", err)
			}
			w.partialBuffer.set(bufferedData)
			break
		} else if err != nil {
			// Try to determine what we failed to decode.
			entry, readErr := ioutil.ReadAll(dec.Buffered())
			if readErr != nil {
				entry = []byte("unknown")
			}
			log.Fatalf("Error when reading logs: %v; Failed entry: %v", err, string(entry))
		}

		w.updateStatus(m)
	}

	return originalLength, nil
}
