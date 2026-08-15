package links

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type BindClassification string

const (
	BindWildcard  BindClassification = "wildcard"
	BindLocalOnly BindClassification = "local-only"
	BindExplicit  BindClassification = "explicit"
)

type Reachability string

const (
	ReachabilityReachable               Reachability = "reachable"
	ReachabilityRespondingAuthenticated Reachability = "responding-authenticated"
	ReachabilityRespondingError         Reachability = "responding-error"
	ReachabilityUnconfirmed             Reachability = "unconfirmed"
	ReachabilityLocalOnly               Reachability = "local-only"
	ReachabilityNotPublished            Reachability = "not-published"
	ReachabilityInvalid                 Reachability = "invalid"
)

type Source string

const (
	SourceManual           Source = "manual"
	SourceLabel            Source = "label"
	SourcePublishedPort    Source = "published-port"
	SourceHostNetworkGuess Source = "host-network-inferred"
)

type PublishedPort struct {
	HostIP        string `json:"hostIp"`
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

type Link struct {
	URL           string       `json:"url"`
	Label         string       `json:"label"`
	Source        Source       `json:"source"`
	Reachability  Reachability `json:"reachability"`
	LocalOnly     bool         `json:"localOnly"`
	HostIP        string       `json:"hostIp,omitempty"`
	HostPort      int          `json:"hostPort,omitempty"`
	ContainerPort int          `json:"containerPort,omitempty"`
}

type ResolveInput struct {
	PublicHost     string
	PublicScheme   string
	PublishedPorts []PublishedPort
	LabelURL       string
	ManualURL      string
}

type ResolveResult struct {
	Primary       *Link
	Links         []Link
	Reachability  Reachability
	InvalidReason string
}

func ClassifyBindAddress(ip string) BindClassification {
	switch strings.TrimSpace(ip) {
	case "", "0.0.0.0", "::":
		return BindWildcard
	case "127.0.0.1", "::1":
		return BindLocalOnly
	default:
		return BindExplicit
	}
}

func ValidateExplicitURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("URL must use http/https and include a host")
	}
	if u.User != nil || u.Fragment != "" {
		return nil, errors.New("URL must not include credentials or fragment")
	}
	if net.ParseIP(u.Hostname()) == nil && strings.ContainsAny(u.Hostname(), " \t\r\n") {
		return nil, errors.New("URL host is invalid")
	}
	return u, nil
}

func Resolve(in ResolveInput) ResolveResult {
	result := ResolveResult{Reachability: ReachabilityNotPublished}
	for _, p := range in.PublishedPorts {
		if strings.ToLower(p.Protocol) != "tcp" || p.HostPort <= 0 {
			continue
		}
		link := Link{Label: "打开服务", Source: SourcePublishedPort, Reachability: ReachabilityUnconfirmed, HostIP: p.HostIP, HostPort: p.HostPort, ContainerPort: p.ContainerPort}
		if ClassifyBindAddress(p.HostIP) == BindLocalOnly {
			link.LocalOnly = true
			link.Reachability = ReachabilityLocalOnly
		} else if in.PublicHost != "" {
			link.URL = schemeOrHTTP(in.PublicScheme) + "://" + net.JoinHostPort(in.PublicHost, strconv.Itoa(p.HostPort))
		}
		result.Links = append(result.Links, link)
	}
	if in.ManualURL != "" {
		if u, err := ValidateExplicitURL(in.ManualURL); err == nil {
			result.Primary = &Link{URL: u.String(), Label: "打开服务", Source: SourceManual, Reachability: ReachabilityReachable}
			result.Reachability = result.Primary.Reachability
			return result
		} else {
			result.InvalidReason = err.Error()
			result.Reachability = ReachabilityInvalid
			return result
		}
	}
	if in.LabelURL != "" {
		if u, err := ValidateExplicitURL(in.LabelURL); err == nil {
			result.Primary = &Link{URL: u.String(), Label: "打开服务", Source: SourceLabel, Reachability: ReachabilityReachable}
			result.Reachability = result.Primary.Reachability
			return result
		} else {
			result.InvalidReason = err.Error()
			result.Reachability = ReachabilityInvalid
			return result
		}
	}
	if len(result.Links) > 0 {
		result.Primary = &result.Links[0]
		result.Reachability = result.Primary.Reachability
	}
	return result
}

func schemeOrHTTP(s string) string {
	if s == "https" {
		return s
	}
	return "http"
}

func MakePublishedURL(scheme, host string, port int) string {
	return schemeOrHTTP(scheme) + "://" + net.JoinHostPort(host, strconv.Itoa(port))
}
