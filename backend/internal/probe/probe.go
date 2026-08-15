package probe

import (
	"context"
	"errors"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Result struct {
	Reachable  bool
	StatusCode int
	Status     string
	URL        string
	Title      string
	IconURL    string
	Err        error
}

type Prober struct{ Client *http.Client }

var titlePattern = regexp.MustCompile(`(?is)<title[^>]*>\s*(.*?)\s*</title>`)
var linkTagPattern = regexp.MustCompile(`(?is)<link\b[^>]*>`)
var attributePattern = regexp.MustCompile(`(?is)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*["']([^"']*)["']`)

func New(timeout time.Duration) *Prober {
	return &Prober{Client: &http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (p *Prober) Probe(ctx context.Context, rawURL string) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return Result{URL: rawURL, Err: err}
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Result{URL: rawURL, Err: context.DeadlineExceeded}
		}
		return Result{URL: rawURL, Err: err}
	}
	if resp.StatusCode == http.StatusMethodNotAllowed {
		resp.Body.Close()
		return p.get(ctx, rawURL)
	}
	result := responseResult(rawURL, resp)
	resp.Body.Close()
	page := p.getTitle(ctx, rawURL)
	result.Title = page.Title
	result.IconURL = page.IconURL
	return result
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
	defer resp.Body.Close()
	result := responseResult(rawURL, resp)
	result.Title, result.IconURL = readPageMeta(resp.Body, rawURL)
	return result
}

func (p *Prober) getTitle(ctx context.Context, rawURL string) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{URL: rawURL, Err: err}
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return Result{URL: rawURL, Err: err}
	}
	defer resp.Body.Close()
	title, icon := readPageMeta(resp.Body, rawURL)
	return Result{URL: rawURL, StatusCode: resp.StatusCode, Status: resp.Status, Title: title, IconURL: icon}
}

func responseResult(rawURL string, resp *http.Response) Result {
	return Result{URL: rawURL, Reachable: true, StatusCode: resp.StatusCode, Status: resp.Status}
}

func readTitle(body io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(body, 128*1024))
	return titleFromHTML(data)
}

func readPageMeta(body io.Reader, rawURL string) (string, string) {
	data, _ := io.ReadAll(io.LimitReader(body, 128*1024))
	return titleFromHTML(data), iconFromHTML(data, rawURL)
}

func titleFromHTML(data []byte) string {
	match := titlePattern.FindSubmatch(data)
	if len(match) < 2 {
		return ""
	}
	title := strings.Join(strings.Fields(html.UnescapeString(string(match[1]))), " ")
	runes := []rune(title)
	if len(runes) > 200 {
		return string(runes[:200])
	}
	return title
}

func iconFromHTML(data []byte, rawURL string) string {
	base, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	var fallback string
	for _, tag := range linkTagPattern.FindAll(data, -1) {
		attrs := make(map[string]string)
		for _, match := range attributePattern.FindAllSubmatch(tag, -1) {
			attrs[strings.ToLower(string(match[1]))] = html.UnescapeString(string(match[2]))
		}
		rel := strings.Fields(strings.ToLower(attrs["rel"]))
		isIcon := false
		for _, value := range rel {
			if value == "icon" || value == "shortcut" || value == "apple-touch-icon" || value == "apple-touch-icon-precomposed" {
				isIcon = true
				break
			}
		}
		if !isIcon || attrs["href"] == "" {
			continue
		}
		resolved, err := base.Parse(attrs["href"])
		if err != nil || (resolved.Scheme != "http" && resolved.Scheme != "https") || resolved.Host == "" {
			continue
		}
		if strings.Contains(strings.ToLower(attrs["rel"]), "icon") {
			return resolved.String()
		}
		if fallback == "" {
			fallback = resolved.String()
		}
	}
	if fallback != "" {
		return fallback
	}
	if base.Scheme == "http" || base.Scheme == "https" {
		base.Path = "/favicon.ico"
		base.RawPath = ""
		base.RawQuery = ""
		base.Fragment = ""
		return base.String()
	}
	return ""
}
