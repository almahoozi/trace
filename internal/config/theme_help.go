package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed theme_help.json
var themeHelpJSON []byte

type ThemeHelpItem struct {
	Key         string `json:"key"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

type themeHelpDoc struct {
	Items []ThemeHelpItem `json:"items"`
}

func ThemeHelpItems() ([]ThemeHelpItem, error) {
	var doc themeHelpDoc
	if err := json.Unmarshal(themeHelpJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse embedded theme help: %w", err)
	}
	items := make([]ThemeHelpItem, 0, len(doc.Items))
	for _, item := range doc.Items {
		key := strings.TrimSpace(item.Key)
		desc := strings.TrimSpace(item.Description)
		kind := strings.ToLower(strings.TrimSpace(item.Kind))
		if key == "" || desc == "" || (kind != "color" && kind != "palette") {
			continue
		}
		items = append(items, ThemeHelpItem{Key: key, Kind: kind, Description: desc})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Key) < strings.ToLower(items[j].Key)
	})
	return items, nil
}

func ThemeHelpForKey(key string) (ThemeHelpItem, bool, error) {
	target := strings.TrimSpace(key)
	if target == "" {
		return ThemeHelpItem{}, false, nil
	}
	items, err := ThemeHelpItems()
	if err != nil {
		return ThemeHelpItem{}, false, err
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Key), target) {
			return item, true, nil
		}
	}
	return ThemeHelpItem{}, false, nil
}
