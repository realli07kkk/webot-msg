package control

import "strings"

type outputSplitter struct {
	tail string
}

type outputEvents struct {
	lines         []string
	prompt        string
	promptChanged bool
}

func (s *outputSplitter) Push(p []byte) outputEvents {
	if len(p) == 0 {
		return outputEvents{prompt: s.tail}
	}

	previousTail := s.tail
	data := string(p)
	if p[0] == '\n' {
		s.tail = ""
	} else {
		data = s.tail + data
	}
	parts := strings.Split(data, "\n")
	var lines []string
	for _, line := range parts[:len(parts)-1] {
		lines = append(lines, strings.TrimSuffix(line, "\r"))
	}

	s.tail = parts[len(parts)-1]
	return outputEvents{
		lines:         lines,
		prompt:        s.tail,
		promptChanged: s.tail != previousTail,
	}
}

func (s *outputSplitter) ResetTail() {
	s.tail = ""
}
