package instructions

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/alanhuangch/agx/internal/contenthash"
)

const (
	BeginMarker = "<!-- BEGIN AGX MANAGED INSTRUCTIONS -->"
	EndMarker   = "<!-- END AGX MANAGED INSTRUCTIONS -->"
)

type Parsed struct {
	Before  []byte
	Managed []byte
	After   []byte
	Found   bool
}

func Compose(fragments [][]byte) ([]byte, error) {
	var output bytes.Buffer
	for index, fragment := range fragments {
		if bytes.Contains(fragment, []byte(BeginMarker)) || bytes.Contains(fragment, []byte(EndMarker)) {
			return nil, fmt.Errorf("instruction source %d contains an AGX managed marker", index)
		}
		if index > 0 {
			output.WriteString("\n\n")
		}
		output.Write(fragment)
	}
	if len(bytes.TrimSpace(output.Bytes())) == 0 {
		return nil, errors.New("instruction content must not be empty")
	}
	return output.Bytes(), nil
}

func Parse(content []byte) (Parsed, error) {
	beginCount := bytes.Count(content, []byte(BeginMarker))
	endCount := bytes.Count(content, []byte(EndMarker))
	if beginCount == 0 && endCount == 0 {
		return Parsed{Before: append([]byte(nil), content...)}, nil
	}
	if beginCount != 1 || endCount != 1 {
		return Parsed{}, errors.New("instructions file contains malformed or duplicate AGX managed markers")
	}
	begin := bytes.Index(content, []byte(BeginMarker))
	end := bytes.Index(content, []byte(EndMarker))
	if begin > end || !markerOnOwnLine(content, begin, len(BeginMarker)) || !markerOnOwnLine(content, end, len(EndMarker)) {
		return Parsed{}, errors.New("instructions file contains malformed AGX managed markers")
	}
	managedStart := begin + len(BeginMarker)
	managedStart = consumeLineEnding(content, managedStart)
	managedEnd := end
	if managedEnd > 0 && content[managedEnd-1] == '\n' {
		managedEnd--
		if managedEnd > 0 && content[managedEnd-1] == '\r' {
			managedEnd--
		}
	}
	blockEnd := end + len(EndMarker)
	blockEnd = consumeLineEnding(content, blockEnd)
	return Parsed{
		Before:  append([]byte(nil), content[:begin]...),
		Managed: append([]byte(nil), content[managedStart:managedEnd]...),
		After:   append([]byte(nil), content[blockEnd:]...),
		Found:   true,
	}, nil
}

func Render(existing, managed []byte) ([]byte, error) {
	parsed, err := Parse(existing)
	if err != nil {
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
	if parsed.Found {
		result := append(parsed.Before, block...)
		result = append(result, parsed.After...)
		return result, nil
	}
	result := append([]byte(nil), existing...)
	if len(result) > 0 {
		if !bytes.HasSuffix(result, []byte("\n")) {
			result = append(result, newline...)
		}
		result = append(result, newline...)
	}
	return append(result, block...), nil
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
	return result, true, nil
}

func DigestManaged(existing []byte) (string, bool, error) {
	parsed, err := Parse(existing)
	if err != nil || !parsed.Found {
		return "", false, err
	}
	return contenthash.Bytes(parsed.Managed), true, nil
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
