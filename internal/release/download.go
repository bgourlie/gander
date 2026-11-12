package release

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"time"
)

// Progress is told how many bytes have arrived, and the total when it's known (else -1).
type Progress func(done, total int64)

// Download fetches url into f, which must be empty, and returns the SHA-256 of
// everything written and its length.
//
// github.com redirects to a signed asset URL that expires within an hour, so every retry
// starts again from url, and from byte zero: Goose's archives are small enough that
// resuming isn't worth it.
func (c *Client) Download(ctx context.Context, url string, f *os.File, progress Progress) (sum []byte, n int64, err error) {
	d := &download{c: c, url: url, f: f, progress: progress, h: sha256.New()}
	backoff := c.Backoff
	for attempt := 0; ; attempt++ {
		err := d.attempt(ctx)
		if err == nil {
			return d.h.Sum(nil), d.n, nil
		}
		var perm *permanentError
		if errors.As(err, &perm) || ctx.Err() != nil || attempt >= c.Retries {
			if perm != nil {
				err = perm.err
			}
			return nil, d.n, fmt.Errorf("downloading %s: %w", url, err)
		}
		select {
		case <-ctx.Done():
			return nil, d.n, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

type download struct {
	c        *Client
	url      string
	f        *os.File
	progress Progress
	h        hash.Hash
	n        int64 // bytes written to f and hashed
}

// permanentError is a failure that retrying won't fix, such as a 404.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }

func (d *download) attempt(ctx context.Context) error {
	if err := d.restart(); err != nil {
		return &permanentError{err}
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.url, nil)
	if err != nil {
		return &permanentError{err}
	}
	req.Header.Set("User-Agent", d.c.UserAgent)
	resp, err := d.c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return errors.New(resp.Status)
	default:
		return &permanentError{errors.New(resp.Status)}
	}

	total := resp.ContentLength // -1 when unknown
	stall := time.AfterFunc(d.c.StallTimeout, func() {
		cancel(fmt.Errorf("nothing arrived for %s", d.c.StallTimeout))
	})
	defer stall.Stop()
	buf := make([]byte, 64<<10)
	for {
		m, rerr := resp.Body.Read(buf)
		if m > 0 {
			stall.Reset(d.c.StallTimeout)
			if _, err := d.f.Write(buf[:m]); err != nil {
				return &permanentError{fmt.Errorf("can't write %s: %w", d.f.Name(), err)}
			}
			d.h.Write(buf[:m])
			d.n += int64(m)
			if d.progress != nil {
				d.progress(d.n, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if cause := context.Cause(ctx); cause != nil && ctx.Err() != nil {
				return cause
			}
			return rerr
		}
	}
	if total >= 0 && d.n != total {
		return fmt.Errorf("the connection closed after %d of %d bytes", d.n, total)
	}
	return nil
}

// restart empties f, so each attempt downloads the whole file.
func (d *download) restart() error {
	if _, err := d.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := d.f.Truncate(0); err != nil {
		return err
	}
	d.h.Reset()
	d.n = 0
	return nil
}
