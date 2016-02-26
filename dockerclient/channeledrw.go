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
