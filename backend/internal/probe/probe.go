package probe

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type Result struct {
	Reachable  bool
	StatusCode int
	Status     string
	URL        string
	Err        error
}

type Prober struct{ Client *http.Client }

func New(timeout time.Duration) *Prober {
	return &Prober{Client: &http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (p *Prober) Probe(ctx context.Context, rawURL string) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return Result{URL: rawURL, Err: err}
	}
	resp, err := p.Client.Do(req)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusMethodNotAllowed {
			return p.get(ctx, rawURL)
		}
		return responseResult(rawURL, resp)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Result{URL: rawURL, Err: context.DeadlineExceeded}
	}
	return Result{URL: rawURL, Err: err}
}
func (p *Prober) get(ctx context.Context, rawURL string) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{URL: rawURL, Err: err}
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return Result{URL: rawURL, Err: err}
	}
	resp.Body.Close()
	return responseResult(rawURL, resp)
}
func responseResult(rawURL string, resp *http.Response) Result {
	return Result{URL: rawURL, Reachable: true, StatusCode: resp.StatusCode, Status: resp.Status}
}
