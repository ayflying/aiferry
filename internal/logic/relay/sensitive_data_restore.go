package relay

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

type sensitiveDataStreamRestorer struct {
	restorer      *sensitiveDataRestorer
	pending       map[string]pendingSensitiveStreamValue
	pendingListed map[string]struct{}
	pendingOrder  []string
}

type pendingSensitiveStreamValue struct {
	value    string
	template []byte
	path     []string
}

func newSensitiveDataStreamRestorer(restorer *sensitiveDataRestorer) *sensitiveDataStreamRestorer {
	if restorer == nil || len(restorer.replacements) == 0 {
		return nil
	}
	return &sensitiveDataStreamRestorer{
		restorer:      restorer,
		pending:       make(map[string]pendingSensitiveStreamValue),
		pendingListed: make(map[string]struct{}),
	}
}

func (r *sensitiveDataRestorer) restoreBufferedResponse(body []byte) []byte {
	if r == nil || len(r.replacements) == 0 || !json.Valid(body) {
		return body
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return body
	}
	restored := restoreSensitiveJSONValue(payload, r.restoreString)
	result, err := json.Marshal(restored)
	if err != nil {
		return body
	}
	return result
}

func (r *sensitiveDataRestorer) restoreString(value string) string {
	if r == nil {
		return value
	}
	// One replacement can contain an earlier placeholder when multiple sensitive
	// patterns overlap, such as a password embedded in an authenticated URL.
	for range len(r.replacements) {
		restored := value
		for placeholder, original := range r.replacements {
			restored = strings.ReplaceAll(restored, placeholder, original)
		}
		if restored == value {
			return restored
		}
		value = restored
	}
	return value
}

func restoreSensitiveJSONValue(value any, restore func(string) string) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			typed[key] = restoreSensitiveJSONValue(item, restore)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = restoreSensitiveJSONValue(item, restore)
		}
		return typed
	case string:
		return restore(typed)
	default:
		return value
	}
}

func (r *sensitiveDataStreamRestorer) restoreSSELine(line []byte) [][]byte {
	if r == nil {
		return [][]byte{line}
	}
	payload, done, valid := sseDataPayload(line)
	if !valid {
		return [][]byte{line}
	}
	if done {
		return append(r.flushPending(), line)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return [][]byte{line}
	}
	eventType := ""
	if object, ok := value.(map[string]any); ok {
		eventType, _ = object["type"].(string)
	}
	newPending := make([]string, 0)
	r.restoreStreamValue(value, value, eventType, nil, &newPending)
	encoded, err := json.Marshal(value)
	if err != nil {
		return [][]byte{line}
	}
	for _, key := range newPending {
		pending := r.pending[key]
		pending.template = append(pending.template[:0], encoded...)
		r.pending[key] = pending
	}
	result := make([][]byte, 0, 1+len(r.pending))
	if isSensitiveStreamTerminal(eventType) {
		result = append(result, r.flushPending()...)
	}
	return append(result, []byte("data: "+string(encoded)+"\n"))
}

func (r *sensitiveDataStreamRestorer) restoreStreamValue(root, value any, eventType string, path []string, newPending *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			r.restoreStreamValue(root, item, eventType, append(path, key), newPending)
		}
	case []any:
		for index, item := range typed {
			r.restoreStreamValue(root, item, eventType, append(path, strconv.Itoa(index)), newPending)
		}
	case string:
		key := eventType + ":" + strings.Join(path, ".")
		pending := r.pending[key]
		if pending.value != "" {
			delete(r.pending, key)
		}
		restored := r.restorer.restoreString(pending.value + typed)
		tail := r.restorer.placeholderPrefixSuffix(restored)
		if tail != "" {
			restored = strings.TrimSuffix(restored, tail)
			if _, listed := r.pendingListed[key]; !listed {
				r.pendingOrder = append(r.pendingOrder, key)
				r.pendingListed[key] = struct{}{}
			}
			r.pending[key] = pendingSensitiveStreamValue{value: tail, path: append([]string(nil), path...)}
			*newPending = append(*newPending, key)
		}
		r.replaceStreamString(root, path, restored)
	}
}

func (r *sensitiveDataStreamRestorer) replaceStreamString(root any, path []string, replacement string) {
	if len(path) == 0 {
		return
	}
	current := root
	for _, key := range path[:len(path)-1] {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[key]
		case []any:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(typed) {
				return
			}
			current = typed[index]
		default:
			return
		}
	}
	last := path[len(path)-1]
	switch typed := current.(type) {
	case map[string]any:
		typed[last] = replacement
	case []any:
		index, err := strconv.Atoi(last)
		if err == nil && index >= 0 && index < len(typed) {
			typed[index] = replacement
		}
	}
}

func (r *sensitiveDataRestorer) placeholderPrefixSuffix(value string) string {
	for placeholder := range r.replacements {
		limit := len(placeholder) - 1
		if len(value) < limit {
			limit = len(value)
		}
		for size := limit; size > 0; size-- {
			candidate := value[len(value)-size:]
			if strings.HasPrefix(placeholder, candidate) {
				return candidate
			}
		}
	}
	return ""
}

func (r *sensitiveDataStreamRestorer) flushPending() [][]byte {
	if r == nil {
		return nil
	}
	result := make([][]byte, 0, len(r.pending))
	for _, key := range r.pendingOrder {
		pending, ok := r.pending[key]
		if !ok || len(pending.template) == 0 {
			continue
		}
		var payload any
		if err := json.Unmarshal(pending.template, &payload); err != nil {
			continue
		}
		r.replaceStreamString(payload, pending.path, pending.value)
		encoded, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		result = append(result, []byte("data: "+string(encoded)+"\n"))
	}
	r.pending = make(map[string]pendingSensitiveStreamValue)
	r.pendingListed = make(map[string]struct{})
	r.pendingOrder = nil
	return result
}

func isSensitiveStreamTerminal(eventType string) bool {
	return eventType == "response.completed" || eventType == "response.failed" || strings.HasSuffix(eventType, ".done")
}
