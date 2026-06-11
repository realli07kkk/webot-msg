package console

import (
	"strings"
	"unicode"
)

func CompleteCommandLine(line string, pos int) (newLine string, newPos int, ok bool) {
	if pos < 0 || pos > len(line) {
		return line, pos, true
	}

	before := line[:pos]
	after := line[pos:]
	if !strings.HasPrefix(before, "/") {
		return line, pos, true
	}

	commandEnd := strings.IndexFunc(before, unicode.IsSpace)
	if commandEnd < 0 {
		return completeCommandName(before, after)
	}

	commandName := before[:commandEnd]
	spec, found := findCommandSpec(commandName)
	if !found || len(spec.Subcommands) == 0 {
		return line, pos, true
	}

	lastSpace := strings.LastIndexFunc(before, unicode.IsSpace)
	subcommandPrefix := before[lastSpace+1:]
	if subcommandPrefix == "" {
		return line, pos, true
	}

	return completeWord(before[:lastSpace+1], subcommandPrefix, after, spec.Subcommands)
}

func completeCommandName(prefix string, after string) (string, int, bool) {
	candidates := make([]string, 0, len(commandSpecs))
	for _, spec := range commandSpecs {
		if strings.HasPrefix(spec.Name, prefix) {
			candidates = append(candidates, spec.Name)
		}
	}
	if len(candidates) == 0 {
		return prefix + after, len(prefix), true
	}

	completed := commonPrefix(candidates)
	if spec, ok := findCommandSpec(completed); ok && len(spec.Subcommands) > 0 && !startsWithSpace(after) {
		completed += " "
	}
	return completed + after, len(completed), true
}

func completeWord(leading string, prefix string, after string, words []string) (string, int, bool) {
	candidates := make([]string, 0, len(words))
	for _, word := range words {
		if strings.HasPrefix(word, prefix) {
			candidates = append(candidates, word)
		}
	}
	if len(candidates) == 0 {
		return leading + prefix + after, len(leading) + len(prefix), true
	}

	completed := commonPrefix(candidates)
	return leading + completed + after, len(leading) + len(completed), true
}

func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) {
			if prefix == "" {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func startsWithSpace(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		return unicode.IsSpace(r)
	}
	return false
}
