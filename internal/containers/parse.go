package containers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseContainers parses one JSON object per line from docker/podman ps --format.
func ParseContainers(data []byte) ([]Container, error) {
	var out []Container
	err := parseLines(data, func(raw map[string]json.RawMessage) error {
		var c Container
		c.ID = firstString(raw, "ID", "Id", "id", "ContainerID")
		c.Names = stringList(raw["Names"])
		if len(c.Names) == 0 {
			c.Names = stringList(raw["Name"])
		}
		c.Image = firstString(raw, "Image", "image")
		c.ImageID = firstString(raw, "ImageID", "ImageId", "ImageID")
		c.Command = firstString(raw, "Command", "command")
		c.CreatedAt = firstString(raw, "CreatedAt", "Created", "CreatedAt")
		c.Created = parseTime(c.CreatedAt)
		c.State = StateFromPS(firstString(raw, "State", "Status"), firstString(raw, "Status"))
		c.Status = firstString(raw, "Status", "State")
		portsRaw := raw["Ports"]
		if len(portsRaw) == 0 {
			portsRaw = raw["ports"]
		}
		c.PortsText, c.Ports = parsePortsValue(portsRaw)
		c.Labels = stringMap(raw["Labels"])
		c.Size = firstString(raw, "Size", "SizeRootFs")
		c.Networks = splitList(firstString(raw, "Networks", "Network"))
		c.Mounts = splitList(firstString(raw, "Mounts"))
		c.Project = firstString(raw, "Project")
		if c.Project == "" {
			c.Project = c.Labels["com.docker.compose.project"]
		}
		if c.Project == "" {
			c.Project = c.Labels["io.podman.compose.project"]
		}
		c.WorkingDir = c.Labels["com.docker.compose.project.working_dir"]
		if c.WorkingDir == "" {
			c.WorkingDir = c.Labels["io.podman.compose.project.working_dir"]
		}
		files := c.Labels["com.docker.compose.project.config_files"]
		if files == "" {
			files = c.Labels["io.podman.compose.project.config_files"]
		}
		for _, file := range strings.Split(files, ",") {
			if file = strings.TrimSpace(file); file != "" {
				c.ConfigFiles = append(c.ConfigFiles, file)
			}
		}
		out = append(out, c)
		return nil
	})
	return out, err
}

// ParseImages parses one JSON object per line from images --format '{{json .}}'.
func ParseImages(data []byte) ([]Image, error) {
	var out []Image
	err := parseLines(data, func(raw map[string]json.RawMessage) error {
		i := Image{ID: firstString(raw, "ID", "Id", "id"), RepoTags: stringList(raw["RepoTags"]), RepoDigests: stringList(raw["RepoDigests"]), CreatedAt: firstString(raw, "CreatedAt", "Created"), Size: firstString(raw, "Size"), VirtualSize: firstString(raw, "VirtualSize"), Labels: stringMap(raw["Labels"])}
		if len(i.RepoTags) == 0 {
			i.RepoTags = stringList(raw["Names"])
		}
		i.Created = parseTime(i.CreatedAt)
		if len(i.RepoTags) == 0 {
			i.RepoTags = stringList(raw["Repository"])
			tag := firstString(raw, "Tag")
			if tag != "" && len(i.RepoTags) > 0 {
				for n := range i.RepoTags {
					if i.RepoTags[n] != "<none>" {
						i.RepoTags[n] += ":" + tag
					}
				}
			}
		}
		out = append(out, i)
		return nil
	})
	return out, err
}

// ParseNetworks parses network list JSON lines.
func ParseNetworks(data []byte) ([]Network, error) {
	var out []Network
	err := parseLines(data, func(raw map[string]json.RawMessage) error {
		n := Network{ID: firstString(raw, "ID", "Id", "id"), Name: firstString(raw, "Name", "name"), Driver: firstString(raw, "Driver", "driver"), Scope: firstString(raw, "Scope", "scope"), Labels: stringMap(raw["Labels"])}
		n.Created = parseTime(firstString(raw, "Created", "CreatedAt"))
		out = append(out, n)
		return nil
	})
	return out, err
}

// ParseVolumes parses docker volume ls and podman volume ls JSON lines.
func ParseVolumes(data []byte) ([]Volume, error) {
	var out []Volume
	err := parseLines(data, func(raw map[string]json.RawMessage) error {
		v := Volume{Name: firstString(raw, "Name", "name"), Driver: firstString(raw, "Driver", "driver"), Mountpoint: firstString(raw, "Mountpoint", "MountPoint"), Scope: firstString(raw, "Scope", "scope"), Size: firstString(raw, "Size"), Labels: stringMap(raw["Labels"])}
		v.Created = parseTime(firstString(raw, "CreatedAt", "Created"))
		out = append(out, v)
		return nil
	})
	return out, err
}

// ParseContexts parses context ls JSON lines.
func ParseContexts(data []byte) ([]Context, error) {
	var out []Context
	err := parseLines(data, func(raw map[string]json.RawMessage) error {
		c := Context{Name: firstString(raw, "Name", "name", "Context"), Current: truthy(firstString(raw, "Current", "CurrentContext", "current")), DockerEndpoint: firstString(raw, "DockerEndpoint", "Docker", "Endpoint", "URI"), Host: firstString(raw, "Host", "HostEndpoint"), Description: firstString(raw, "Description"), Error: firstString(raw, "Error")}
		c.TLS = truthy(firstString(raw, "TLS", "TLSData"))
		out = append(out, c)
		return nil
	})
	return out, err
}

// ParseStats parses docker stats --no-stream JSON lines.
func ParseStats(data []byte) ([]Stat, error) {
	var out []Stat
	err := parseLines(data, func(raw map[string]json.RawMessage) error {
		out = append(out, Stat{Container: firstString(raw, "Container", "Name"), ID: firstString(raw, "ID", "Id"), Name: firstString(raw, "Name"), CPUPerc: firstString(raw, "CPUPerc", "CPU %"), MemUsage: firstString(raw, "MemUsage", "Mem Usage"), MemPerc: firstString(raw, "MemPerc", "Mem %"), NetIO: firstString(raw, "NetIO", "Net I/O"), BlockIO: firstString(raw, "BlockIO", "Block I/O"), PIDs: firstString(raw, "PIDs")})
		return nil
	})
	return out, err
}

// ParseEvents parses docker events --format JSON lines.
func ParseEvents(data []byte) ([]Event, error) {
	var out []Event
	err := parseLines(data, func(raw map[string]json.RawMessage) error {
		e := Event{Type: firstString(raw, "Type", "type"), Action: firstString(raw, "Action", "Status", "action"), Actor: firstString(raw, "Actor.Attributes.name", "Actor", "Name"), Labels: stringMap(raw["Actor.Attributes"])}
		if e.Actor == "" {
			e.Actor = e.Labels["name"]
		}
		e.Time = parseTime(firstString(raw, "time", "Time", "timeNano"))
		out = append(out, e)
		return nil
	})
	return out, err
}

// ParseHistory parses docker history --format JSON lines.
func ParseHistory(data []byte) ([]HistoryEntry, error) {
	var out []HistoryEntry
	err := parseLines(data, func(raw map[string]json.RawMessage) error {
		h := HistoryEntry{ID: firstString(raw, "ID", "Id"), CreatedAt: firstString(raw, "CreatedAt", "Created"), CreatedBy: firstString(raw, "CreatedBy"), Size: firstString(raw, "Size"), Comment: firstString(raw, "Comment")}
		h.Created = parseTime(h.CreatedAt)
		out = append(out, h)
		return nil
	})
	return out, err
}

// ParseVersion accepts the object returned by docker version --format json or line JSON.
func ParseVersion(data []byte) (Version, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Version{}, err
	}
	var v Version
	v.Client = firstString(raw, "Client.Version", "ClientVersion", "Version")
	v.Server = firstString(raw, "Server.Version", "ServerVersion")
	v.Version = firstString(raw, "Version", "Client.Version")
	v.APIVersion = firstString(raw, "ApiVersion", "APIVersion", "Client.ApiVersion")
	return v, nil
}

// ParseInspect parses an inspect JSON value into a generic map while retaining arbitrary fields.
func ParseInspect(data []byte) (any, error) {
	var v any
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("empty inspect JSON")
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// ParsePortsText parses the human-readable Ports field from ps output.
func ParsePortsText(text string) []Port {
	var ports []Port
	for _, item := range strings.Split(text, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "->", 2)
		if len(parts) == 1 {
			private, proto := portNumber(parts[0])
			if private > 0 {
				ports = append(ports, Port{PrivatePort: private, Type: proto})
			}
			continue
		}
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		ip, public := splitHostPort(left)
		private, proto := portNumber(right)
		if public > 0 && private > 0 {
			ports = append(ports, Port{IP: ip, PublicPort: public, PrivatePort: private, Type: proto})
		}
	}
	return ports
}

func parsePortsValue(raw json.RawMessage) (string, []Port) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, ParsePortsText(text)
	}
	var entries []map[string]json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return "", nil
	}
	ports := make([]Port, 0, len(entries))
	for _, entry := range entries {
		p := Port{
			IP:          firstString(entry, "HostIP", "host_ip", "hostIP"),
			PrivatePort: firstInt(entry, "ContainerPort", "container_port", "containerPort"),
			PublicPort:  firstInt(entry, "HostPort", "host_port", "hostPort"),
			Type:        firstString(entry, "Protocol", "protocol", "Type"),
		}
		if p.Type == "" {
			p.Type = "tcp"
		}
		if p.PrivatePort > 0 {
			ports = append(ports, p)
		}
	}
	return "", ports
}

func parseLines(data []byte, fn func(map[string]json.RawMessage) error) error {
	s := bufio.NewScanner(strings.NewReader(string(data)))
	s.Buffer(make([]byte, 4096), 4*1024*1024)
	line := 0
	for s.Scan() {
		line++
		text := strings.TrimSpace(s.Text())
		if text == "" {
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(text), &raw); err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
		if raw == nil {
			continue
		}
		if err := fn(raw); err != nil {
			return fmt.Errorf("line %d: %w", line, err)
		}
	}
	return s.Err()
}

func firstString(raw map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		v, ok := raw[key]
		if !ok && strings.Contains(key, ".") {
			parts := strings.Split(key, ".")
			current := raw
			for i, p := range parts {
				v, ok = current[p]
				if !ok {
					break
				}
				if i < len(parts)-1 {
					var child map[string]json.RawMessage
					if json.Unmarshal(v, &child) != nil {
						ok = false
						break
					}
					current = child
				}
			}
			if !ok {
				continue
			}
		}
		if ok {
			var s string
			if json.Unmarshal(v, &s) == nil {
				return strings.TrimSpace(s)
			}
			var b bool
			if json.Unmarshal(v, &b) == nil {
				return strconv.FormatBool(b)
			}
			var n json.Number
			if json.Unmarshal(v, &n) == nil {
				return n.String()
			}
		}
	}
	return ""
}

func firstInt(raw map[string]json.RawMessage, keys ...string) int {
	for _, key := range keys {
		if v, ok := raw[key]; ok {
			var n int
			if json.Unmarshal(v, &n) == nil {
				return n
			}
			var s string
			if json.Unmarshal(v, &s) == nil {
				n, _ = strconv.Atoi(s)
				return n
			}
		}
	}
	return 0
}
func stringList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		for i := range list {
			list[i] = strings.TrimSpace(list[i])
		}
		return list
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return splitList(s)
	}
	return nil
}
func stringMap(raw json.RawMessage) map[string]string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var m map[string]string
	if json.Unmarshal(raw, &m) == nil {
		return m
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	m = map[string]string{}
	key, value := "", ""
	flush := func() {
		if key != "" {
			m[key] = value
		}
	}
	for _, pair := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok {
			flush()
			key, value = strings.TrimSpace(k), strings.TrimSpace(v)
		} else if key != "" {
			value += "," + strings.TrimSpace(pair)
		}
	}
	flush()
	return m
}
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if n, e := strconv.ParseInt(s, 10, 64); e == nil {
		if n > 1e12 {
			return time.Unix(0, n)
		}
		return time.Unix(n, 0)
	}
	layouts := []string{time.RFC3339Nano, "2006-01-02 15:04:05 -0700 MST", "2006-01-02 15:04:05 -0700", "2006-01-02 15:04:05", "2006-01-02T15:04:05.000000000Z07:00"}
	for _, layout := range layouts {
		if t, e := time.Parse(layout, s); e == nil {
			return t
		}
	}
	return time.Time{}
}
func truthy(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "yes" || s == "*" || s == "1"
}
func portNumber(s string) (int, string) {
	p := strings.SplitN(strings.TrimSpace(s), "/", 2)
	n, e := strconv.Atoi(p[0])
	proto := "tcp"
	if len(p) == 2 && p[1] != "" {
		proto = p[1]
	}
	if e != nil {
		return 0, proto
	}
	return n, proto
}
func splitHostPort(s string) (string, int) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return "", 0
	}
	n, e := strconv.Atoi(parts[len(parts)-1])
	if e != nil {
		return "", 0
	}
	return strings.Join(parts[:len(parts)-1], ":"), n
}
