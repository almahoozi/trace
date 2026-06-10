package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type searchPrompt struct {
	input  []rune
	cursor int
}

func newSearchPrompt(initial string) *searchPrompt {
	r := []rune(initial)
	return &searchPrompt{input: r, cursor: len(r)}
}

func (s *searchPrompt) value() string {
	if s == nil {
		return ""
	}
	return string(s.input)
}

func (s *searchPrompt) applyKey(keyMsg tea.KeyMsg) bool {
	if s == nil {
		return false
	}
	switch keyMsg.String() {
	case "left":
		if s.cursor > 0 {
			s.cursor--
		}
		return true
	case "right":
		if s.cursor < len(s.input) {
			s.cursor++
		}
		return true
	case "home":
		s.cursor = 0
		return true
	case "end":
		s.cursor = len(s.input)
		return true
	case "backspace":
		if s.cursor > 0 {
			s.input = append(s.input[:s.cursor-1], s.input[s.cursor:]...)
			s.cursor--
		}
		return true
	case "delete":
		if s.cursor < len(s.input) {
			s.input = append(s.input[:s.cursor], s.input[s.cursor+1:]...)
		}
		return true
	}

	if keyMsg.Type == tea.KeyRunes {
		insert := keyMsg.Runes
		if len(insert) == 0 {
			return true
		}
		left := append([]rune{}, s.input[:s.cursor]...)
		right := append([]rune{}, s.input[s.cursor:]...)
		left = append(left, insert...)
		left = append(left, right...)
		s.input = left
		s.cursor += len(insert)
		return true
	}

	return false
}

func (s *searchPrompt) viewLine() string {
	if s == nil {
		return "/"
	}
	r := s.input
	if s.cursor < 0 {
		s.cursor = 0
	}
	if s.cursor > len(r) {
		s.cursor = len(r)
	}
	text := string(r)
	if s.cursor == len(r) {
		return "/" + text + "|"
	}
	return fmt.Sprintf("/%s|%s", string(r[:s.cursor]), string(r[s.cursor:]))
}

func searchHint() string {
	return strings.Join([]string{
		"enter apply",
		"esc cancel",
		"=exact",
		"/re/i regex",
		"$.field=value",
		"&& and",
	}, " | ")
}
