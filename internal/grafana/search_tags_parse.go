package grafana

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func parseTraceSearchTagsPayload(body []byte) ([]string, error) {
	var asObject struct {
		Tags []string `json:"tags"`
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(body, &asObject); err == nil {
		if len(asObject.Tags) > 0 {
			return dedupeTags(asObject.Tags), nil
		}
		if len(asObject.Data) > 0 {
			return dedupeTags(asObject.Data), nil
		}
	}

	var asStrings []string
	if err := json.Unmarshal(body, &asStrings); err == nil {
		return dedupeTags(asStrings), nil
	}

	var asAny []any
	if err := json.Unmarshal(body, &asAny); err == nil {
		out := make([]string, 0, len(asAny))
		for _, item := range asAny {
			switch t := item.(type) {
			case string:
				out = append(out, t)
			case map[string]any:
				if s := strings.TrimSpace(fmt.Sprint(t["key"])); s != "" {
					out = append(out, s)
					continue
				}
				if s := strings.TrimSpace(fmt.Sprint(t["name"])); s != "" {
					out = append(out, s)
					continue
				}
				if s := strings.TrimSpace(fmt.Sprint(t["value"])); s != "" {
					out = append(out, s)
				}
			}
		}
		if len(out) > 0 {
			return dedupeTags(out), nil
		}
	}

	return nil, fmt.Errorf("parse trace search tags payload")
}

func dedupeTags(tags []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}
