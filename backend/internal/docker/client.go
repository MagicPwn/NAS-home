package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func NewClient(dockerHost string, timeout time.Duration) (*Client, error) {
	if strings.HasPrefix(dockerHost, "unix://") {
		socket := strings.TrimPrefix(dockerHost, "unix://")
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: timeout}).DialContext(ctx, "unix", socket)
		}}
		return &Client{httpClient: &http.Client{Transport: transport, Timeout: timeout}, baseURL: "http://docker"}, nil
	}
	if strings.HasPrefix(dockerHost, "tcp://") {
		dockerHost = "http://" + strings.TrimPrefix(dockerHost, "tcp://")
	}
	if strings.HasPrefix(dockerHost, "http://") || strings.HasPrefix(dockerHost, "https://") {
		return &Client{httpClient: &http.Client{Timeout: timeout}, baseURL: strings.TrimRight(dockerHost, "/")}, nil
	}
	return nil, errors.New("Docker host must use unix://, tcp://, http:// or https://")
}

type Summary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}
type Inspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		Paused     bool   `json:"Paused"`
		Restarting bool   `json:"Restarting"`
	} `json:"State"`
	NetworkSettings struct {
		Ports map[string][]PortBinding `json:"Ports"`
	} `json:"NetworkSettings"`
}

func (c *Client) request(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("docker API returned " + resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_ping", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("docker API returned " + resp.Status)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}
func (c *Client) ListContainers(ctx context.Context) ([]Summary, error) {
	var out []Summary
	err := c.request(ctx, "/containers/json?all=true", &out)
	return out, err
}
func (c *Client) InspectContainer(ctx context.Context, id string) (Inspect, error) {
	var out Inspect
	err := c.request(ctx, "/containers/"+url.PathEscape(id)+"/json", &out)
	return out, err
}
