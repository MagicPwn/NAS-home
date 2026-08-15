package docker

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type PortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type PublishedPort struct {
	HostIP        string `json:"hostIp"`
	HostPort      int    `json:"hostPort"`
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

type ContainerMetadata struct {
	ID     string
	Name   string
	Labels map[string]string
}

func CleanContainerName(name string) string { return strings.TrimPrefix(name, "/") }

func StableServiceKey(c ContainerMetadata) string {
	if key := strings.TrimSpace(c.Labels["nas.home.key"]); key != "" {
		return key
	}
	project := strings.TrimSpace(c.Labels["com.docker.compose.project"])
	service := strings.TrimSpace(c.Labels["com.docker.compose.service"])
	if project != "" && service != "" {
		return project + "/" + service
	}
	if name := CleanContainerName(c.Name); name != "" {
		return name
	}
	return c.ID
}

func IsEnabled(labels map[string]string) bool {
	return strings.ToLower(strings.TrimSpace(labels["nas.home.enabled"])) != "false"
}

func ParsePublishedPorts(input map[string][]PortBinding) ([]PublishedPort, error) {
	result := make([]PublishedPort, 0)
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts := strings.Split(key, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid port key %q", key)
		}
		containerPort, err := strconv.Atoi(parts[0])
		if err != nil || containerPort <= 0 {
			return nil, fmt.Errorf("invalid container port %q", key)
		}
		protocol := strings.ToLower(parts[1])
		if protocol != "tcp" {
			continue
		}
		bindings := append([]PortBinding(nil), input[key]...)
		sort.SliceStable(bindings, func(i, j int) bool {
			if bindings[i].HostIP != bindings[j].HostIP {
				return bindings[i].HostIP < bindings[j].HostIP
			}
			return bindings[i].HostPort < bindings[j].HostPort
		})
		for _, binding := range bindings {
			hostPort, err := strconv.Atoi(binding.HostPort)
			if err != nil || hostPort <= 0 {
				return nil, fmt.Errorf("invalid host port %q", binding.HostPort)
			}
			candidate := PublishedPort{HostIP: binding.HostIP, HostPort: hostPort, ContainerPort: containerPort, Protocol: protocol}
			duplicate := false
			for _, existing := range result {
				if existing.HostPort == candidate.HostPort && existing.ContainerPort == candidate.ContainerPort && existing.Protocol == candidate.Protocol {
					duplicate = true
					break
				}
			}
			if !duplicate {
				result = append(result, candidate)
			}
		}
	}
	return result, nil
}
