package docker

import "bytes"

// maxExecCaptureBytes bounds how much stdout/stderr (each) the runtime buffers
// from an exec. Without a cap, a command that emits gigabytes (or /dev/zero)
// would OOM the control plane, since StdCopy buffers the whole stream. 1 MiB is
// far more than any diagnostic command needs.
const maxExecCaptureBytes = 1 << 20

// cappedBuffer is an io.Writer that retains at most limit bytes and discards the
// rest, while still reporting every Write as fully consumed. Reporting the full
// length is important: stdcopy/io.Copy treat a short write (n < len(p)) as
// io.ErrShortWrite and abort the copy, which would surface a spurious error.
// Instead we silently drop the overflow and expose Truncated() so callers can
// flag clipped output.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

// Write appends up to limit total bytes and reports the full input length as
// written. Bytes beyond the limit are dropped and Truncated() becomes true.
func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := c.limit - c.buf.Len()
	if remaining <= 0 {
		if len(p) > 0 {
			c.truncated = true
		}
		return len(p), nil
	}
	if len(p) > remaining {
		c.buf.Write(p[:remaining])
		c.truncated = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

// String returns the retained bytes.
func (c *cappedBuffer) String() string { return c.buf.String() }

// Truncated reports whether any bytes were dropped.
func (c *cappedBuffer) Truncated() bool { return c.truncated }
