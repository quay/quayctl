// Copyright 2015 CoreOS, Inc.
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

import "io"

// channeledrw is a reader-writer backed by a channel for concurrent blocking reading
// and writing.
type channeledrw struct {
	channel chan byte
	count   uint64
}

// newChanneledRW creates a new channel-backed reader-writer.
func newChanneledRW() *channeledrw {
	return &channeledrw{
		channel: make(chan byte, 1024*1024*1024),
		count:   0,
	}
}

// ReadCount returns the number of bytes that have been read from the reader.
func (c *channeledrw) ReadCount() uint64 {
	return c.count
}

// DoneWriting marks the channel as closed for writing.
func (c *channeledrw) DoneWriting() {
	close(c.channel)
}

func (c *channeledrw) Read(p []byte) (n int, err error) {
	for i := 0; i < len(p); i++ {
		b, ok := <-c.channel

		if !ok {
			return i, io.EOF
		}

		p[i] = b
		c.count = c.count + 1
	}

	return len(p), nil
}

func (c *channeledrw) Write(p []byte) (n int, err error) {
	var index = 0
	for _, b := range p {
		c.channel <- b
		index = index + 1
	}

	return index, nil
}
