package proxy

import "io"

// blockingPipe returns a reader that blocks until written to or closed,
// standing in for a stdin that the agent is holding open.
func blockingPipe() (io.ReadCloser, io.WriteCloser) {
	return io.Pipe()
}
