package mcpconfig

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/alanhuangch/agx/internal/contenthash"
)

const (
	BeginMarker = "# BEGIN AGX MANAGED MCP SERVERS"
	EndMarker   = "# END AGX MANAGED MCP SERVERS"
)

type Server struct {
	Executable string
	Args       []string
	EnvVars    []string
}

type Parsed struct {
	Before  []byte
	Managed []byte
	After   []byte
	Found   bool
}

func Compose(servers map[string]Server) ([]byte, error) {
	if len(servers) == 0 {
		return nil, errors.New("MCP server configuration must not be empty")
	}
	var output strings.Builder
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for index, name := range names {
		server := servers[name]
		if index > 0 {
			output.WriteString("\n\n")
		}
		fmt.Fprintf(&output, "[mcp_servers.%s]\n", strconv.Quote(name))
		fmt.Fprintf(&output, "command = %s\n", strconv.Quote(server.Executable))
		if len(server.Args) != 0 {
			output.WriteString("args = ")
			writeStringArray(&output, server.Args)
			output.WriteByte('\n')
		}
		if len(server.EnvVars) != 0 {
			envVars := append([]string(nil), server.EnvVars...)
			sort.Strings(envVars)
			output.WriteString("env_vars = ")
			writeStringArray(&output, envVars)
			output.WriteByte('\n')
		}
	}
	managed := []byte(strings.TrimSuffix(output.String(), "\n"))
	if err := validateTOML(managed); err != nil {
		return nil, fmt.Errorf("render MCP server configuration: %w", err)
	}
	return managed, nil
}

func Parse(content []byte) (Parsed, error) {
	beginCount := bytes.Count(content, []byte(BeginMarker))
	endCount := bytes.Count(content, []byte(EndMarker))
	if beginCount == 0 && endCount == 0 {
		return Parsed{Before: append([]byte(nil), content...)}, nil
	}
	if beginCount != 1 || endCount != 1 {
		return Parsed{}, errors.New("codex config contains malformed or duplicate AGX MCP markers")
	}
	begin := bytes.Index(content, []byte(BeginMarker))
	end := bytes.Index(content, []byte(EndMarker))
	if begin > end || !markerOnOwnLine(content, begin, len(BeginMarker)) || !markerOnOwnLine(content, end, len(EndMarker)) {
		return Parsed{}, errors.New("codex config contains malformed AGX MCP markers")
	}
	managedStart := consumeLineEnding(content, begin+len(BeginMarker))
	managedEnd := end
	if managedEnd > 0 && content[managedEnd-1] == '\n' {
		managedEnd--
		if managedEnd > 0 && content[managedEnd-1] == '\r' {
			managedEnd--
		}
	}
	blockEnd := consumeLineEnding(content, end+len(EndMarker))
	return Parsed{
		Before:  append([]byte(nil), content[:begin]...),
		Managed: append([]byte(nil), content[managedStart:managedEnd]...),
		After:   append([]byte(nil), content[blockEnd:]...),
		Found:   true,
	}, nil
}

func Render(existing, managed []byte, names []string) ([]byte, error) {
	parsed, err := Parse(existing)
	if err != nil {
		return nil, err
	}
	outside := append(append([]byte(nil), parsed.Before...), parsed.After...)
	decoded, err := decodeTOML(outside)
	if err != nil {
		return nil, fmt.Errorf("existing Codex config is invalid TOML: %w", err)
	}
	if err := rejectUnmanagedCollisions(decoded, names); err != nil {
		return nil, err
	}
	newline := []byte("\n")
	if bytes.Contains(existing, []byte("\r\n")) {
		newline = []byte("\r\n")
	}
	block := make([]byte, 0, len(managed)+len(BeginMarker)+len(EndMarker)+4*len(newline))
	block = append(block, BeginMarker...)
	block = append(block, newline...)
	block = append(block, managed...)
	block = append(block, newline...)
	block = append(block, EndMarker...)
	block = append(block, newline...)
	var result []byte
	if parsed.Found {
		result = append(append(parsed.Before, block...), parsed.After...)
	} else {
		result = append([]byte(nil), existing...)
		if len(result) > 0 {
			if !bytes.HasSuffix(result, []byte("\n")) {
				result = append(result, newline...)
			}
			result = append(result, newline...)
		}
		result = append(result, block...)
	}
	if err := validateTOML(result); err != nil {
		return nil, fmt.Errorf("combined Codex config is invalid TOML: %w", err)
	}
	return result, nil
}

func Remove(existing []byte) ([]byte, bool, error) {
	parsed, err := Parse(existing)
	if err != nil || !parsed.Found {
		return existing, false, err
	}
	before := parsed.Before
	if len(parsed.After) == 0 {
		before = removeOneBlankLine(before)
	}
	result := append(before, parsed.After...)
	if len(bytes.TrimSpace(result)) == 0 {
		return nil, true, nil
	}
	if err := validateTOML(result); err != nil {
		return nil, false, fmt.Errorf("codex config outside the AGX MCP block is invalid TOML: %w", err)
	}
	return result, true, nil
}

func DigestManaged(existing []byte) (string, bool, error) {
	parsed, err := Parse(existing)
	if err != nil || !parsed.Found {
		return "", false, err
	}
	if err := validateTOML(parsed.Managed); err != nil {
		return "", false, fmt.Errorf("managed MCP configuration is invalid TOML: %w", err)
	}
	return contenthash.Bytes(parsed.Managed), true, nil
}

func ServerNames(managed []byte) ([]string, error) {
	decoded, err := decodeTOML(managed)
	if err != nil {
		return nil, err
	}
	value, exists := decoded["mcp_servers"]
	if !exists {
		return nil, errors.New("managed MCP configuration has no mcp_servers table")
	}
	servers, ok := value.(map[string]any)
	if !ok || len(servers) == 0 {
		return nil, errors.New("managed MCP configuration has an invalid mcp_servers table")
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func writeStringArray(output *strings.Builder, values []string) {
	output.WriteByte('[')
	for index, value := range values {
		if index > 0 {
			output.WriteString(", ")
		}
		output.WriteString(strconv.Quote(value))
	}
	output.WriteByte(']')
}

func rejectUnmanagedCollisions(decoded map[string]any, names []string) error {
	value, exists := decoded["mcp_servers"]
	if !exists {
		return nil
	}
	servers, ok := value.(map[string]any)
	if !ok {
		return errors.New("existing Codex mcp_servers configuration cannot be extended safely")
	}
	for _, name := range names {
		if _, exists := servers[name]; exists {
			return fmt.Errorf("codex MCP server %q is already configured outside the AGX managed block", name)
		}
	}
	return nil
}

func decodeTOML(content []byte) (map[string]any, error) {
	decoded := make(map[string]any)
	if len(bytes.TrimSpace(content)) == 0 {
		return decoded, nil
	}
	_, err := toml.Decode(string(content), &decoded)
	return decoded, err
}

func validateTOML(content []byte) error {
	_, err := decodeTOML(content)
	return err
}

func markerOnOwnLine(content []byte, start, length int) bool {
	if start > 0 && content[start-1] != '\n' {
		return false
	}
	end := start + length
	return end == len(content) || content[end] == '\n' || (content[end] == '\r' && end+1 < len(content) && content[end+1] == '\n')
}

func consumeLineEnding(content []byte, index int) int {
	if index < len(content) && content[index] == '\r' {
		index++
	}
	if index < len(content) && content[index] == '\n' {
		index++
	}
	return index
}

func removeOneBlankLine(content []byte) []byte {
	value := string(content)
	if strings.HasSuffix(value, "\r\n\r\n") {
		return []byte(strings.TrimSuffix(value, "\r\n"))
	}
	if strings.HasSuffix(value, "\n\n") {
		return []byte(strings.TrimSuffix(value, "\n"))
	}
	return content
}
