package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

type ImportChange struct {
	Path string
	From any
	To   any
}

func ExportNonDefault(cfg Config) ([]byte, error) {
	current, err := configToJSONMap(cfg)
	if err != nil {
		return nil, err
	}
	defaults, err := configToJSONMap(DefaultConfig())
	if err != nil {
		return nil, err
	}

	patchAny, ok := diffAny(defaults, current)
	if !ok {
		return []byte("{}\n"), nil
	}
	patch, ok := patchAny.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("export patch is not an object")
	}

	buf, err := json.MarshalIndent(patch, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(buf, '\n'), nil
}

func ApplyImport(cfg Config, data []byte) (Config, error) {
	overlay, err := parseOverlay(data)
	if err != nil {
		return Config{}, err
	}

	current, err := configToJSONMap(cfg)
	if err != nil {
		return Config{}, err
	}
	merged := mergeMaps(current, overlay)

	buf, err := json.Marshal(merged)
	if err != nil {
		return Config{}, err
	}

	updated := cfg
	if err := json.Unmarshal(buf, &updated); err != nil {
		return Config{}, fmt.Errorf("invalid merged config json: %w", err)
	}
	if err := updated.validate(); err != nil {
		return Config{}, err
	}
	return updated, nil
}

func DiffImport(cfg Config, data []byte) ([]ImportChange, error) {
	overlay, err := parseOverlay(data)
	if err != nil {
		return nil, err
	}
	current, err := configToJSONMap(cfg)
	if err != nil {
		return nil, err
	}
	merged := mergeMaps(current, overlay)
	changes := make([]ImportChange, 0)
	collectChanges("", current, merged, &changes)
	return changes, nil
}

func parseOverlay(data []byte) (map[string]any, error) {
	var overlay map[string]any
	if err := json.Unmarshal(data, &overlay); err != nil {
		return nil, fmt.Errorf("invalid import json: %w", err)
	}
	if overlay == nil {
		overlay = map[string]any{}
	}
	return overlay, nil
}

func configToJSONMap(cfg Config) (map[string]any, error) {
	buf, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func diffAny(defaultValue, currentValue any) (any, bool) {
	defaultMap, defaultIsMap := defaultValue.(map[string]any)
	currentMap, currentIsMap := currentValue.(map[string]any)
	if defaultIsMap && currentIsMap {
		out := map[string]any{}
		for key, currentEntry := range currentMap {
			defaultEntry, ok := defaultMap[key]
			if !ok {
				out[key] = currentEntry
				continue
			}
			if diff, include := diffAny(defaultEntry, currentEntry); include {
				out[key] = diff
			}
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	}

	defaultSlice, defaultIsSlice := defaultValue.([]any)
	currentSlice, currentIsSlice := currentValue.([]any)
	if defaultIsSlice && currentIsSlice {
		if reflect.DeepEqual(defaultSlice, currentSlice) {
			return nil, false
		}

		if len(currentSlice) == 0 {
			return currentSlice, true
		}

		var defaultElem map[string]any
		if len(defaultSlice) > 0 {
			if m, ok := defaultSlice[0].(map[string]any); ok {
				defaultElem = m
			}
		}

		if defaultElem != nil {
			trimmed := make([]any, 0, len(currentSlice))
			for _, item := range currentSlice {
				itemMap, ok := item.(map[string]any)
				if !ok {
					return currentSlice, true
				}
				diffed, include := diffAny(defaultElem, itemMap)
				if !include {
					trimmed = append(trimmed, map[string]any{})
					continue
				}
				trimmedMap, ok := diffed.(map[string]any)
				if !ok {
					return currentSlice, true
				}
				trimmed = append(trimmed, trimmedMap)
			}
			return trimmed, true
		}

		return currentSlice, true
	}

	if reflect.DeepEqual(defaultValue, currentValue) {
		return nil, false
	}
	return currentValue, true
}

func mergeMaps(base, overlay map[string]any) map[string]any {
	merged := make(map[string]any, len(base))
	for key, value := range base {
		merged[key] = value
	}
	for key, overlayValue := range overlay {
		if existingValue, ok := merged[key]; ok {
			existingMap, existingIsMap := existingValue.(map[string]any)
			overlayMap, overlayIsMap := overlayValue.(map[string]any)
			if existingIsMap && overlayIsMap {
				merged[key] = mergeMaps(existingMap, overlayMap)
				continue
			}
		}
		merged[key] = overlayValue
	}
	return merged
}

func collectChanges(path string, from, to any, out *[]ImportChange) {
	fromMap, fromIsMap := from.(map[string]any)
	toMap, toIsMap := to.(map[string]any)
	if fromIsMap && toIsMap {
		keySet := map[string]struct{}{}
		for key := range fromMap {
			keySet[key] = struct{}{}
		}
		for key := range toMap {
			keySet[key] = struct{}{}
		}
		keys := make([]string, 0, len(keySet))
		for key := range keySet {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			nextPath := key
			if path != "" {
				nextPath = path + "." + key
			}
			collectChanges(nextPath, fromMap[key], toMap[key], out)
		}
		return
	}

	if reflect.DeepEqual(from, to) {
		return
	}
	*out = append(*out, ImportChange{Path: path, From: from, To: to})
}
