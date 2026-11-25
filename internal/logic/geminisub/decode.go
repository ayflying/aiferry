package geminisub

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/gogf/gf/v2/errors/gerror"
)

const maxEventBytes = 4 << 20

type upstreamResponse struct {
	Error      json.RawMessage   `json:"error"`
	Response   *upstreamResponse `json:"response"`
	Candidates []struct {
		FinishReason string `json:"finishReason"`
		Content      struct {
			Parts []struct {
				Text         *string         `json:"text"`
				Thought      bool            `json:"thought"`
				InlineData   json.RawMessage `json:"inlineData"`
				FileData     json.RawMessage `json:"fileData"`
				FunctionCall json.RawMessage `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Usage *struct {
		Prompt     *int `json:"promptTokenCount"`
		Candidates *int `json:"candidatesTokenCount"`
		Thoughts   *int `json:"thoughtsTokenCount"`
		Total      *int `json:"totalTokenCount"`
		Cached     *int `json:"cachedContentTokenCount"`
	} `json:"usageMetadata"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

func readUpstream(src io.Reader, visit func(upstreamResponse) error) (terminal bool, err error) {
	reader := bufio.NewReader(src)
	for {
		ch, err := reader.ReadByte()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, gerror.Wrap(err, "read gemini subscription response")
		}
		switch ch {
		case ' ', '\t', '\r', '\n':
			continue
		}
		if err := reader.UnreadByte(); err != nil {
			return false, gerror.Wrap(err, "read gemini subscription response")
		}
		if ch == '{' || ch == '[' {
			return true, readJSON(reader, ch == '[', visit)
		}
		return readSSE(reader, visit)
	}
}

func readJSON(src io.Reader, array bool, visit func(upstreamResponse) error) error {
	decoder := json.NewDecoder(src)
	if array {
		if _, err := decoder.Token(); err != nil {
			return gerror.New("invalid gemini subscription JSON array")
		}
	}
	for !array || decoder.More() {
		var response upstreamResponse
		if err := decoder.Decode(&response); err != nil {
			return gerror.New("invalid gemini subscription JSON response")
		}
		if err := visit(response); err != nil {
			return err
		}
		if !array {
			break
		}
	}
	if array {
		token, err := decoder.Token()
		if err != nil || token != json.Delim(']') {
			return gerror.New("invalid gemini subscription JSON array")
		}
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return gerror.New("unexpected data after gemini subscription JSON response")
	}
	return nil
}

func readSSE(src io.Reader, visit func(upstreamResponse) error) (bool, error) {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 16<<10), maxEventBytes)
	var data []byte
	flush := func() (bool, error) {
		payload := bytes.TrimSpace(data)
		data = data[:0]
		if len(payload) == 0 {
			return false, nil
		}
		if bytes.Equal(payload, []byte("[DONE]")) {
			return true, nil
		}
		var response upstreamResponse
		if err := json.Unmarshal(payload, &response); err != nil {
			return false, gerror.New("invalid gemini subscription SSE data")
		}
		return false, visit(response)
	}
	first := true
	for scanner.Scan() {
		line := scanner.Bytes()
		if first {
			line = bytes.TrimPrefix(line, []byte("\xef\xbb\xbf"))
			first = false
		}
		if len(line) == 0 {
			if done, err := flush(); done || err != nil {
				return done, err
			}
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		// Some upstreams omit the blank line between complete JSON data frames.
		// An incomplete payload can still span multiple data lines per SSE.
		if len(data) > 0 && (json.Valid(data) || bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]"))) {
			if done, err := flush(); done || err != nil {
				return done, err
			}
		}
		if len(data)+len(line) > maxEventBytes {
			return false, gerror.New("gemini subscription SSE event exceeds size limit")
		}
		if len(data) > 0 {
			data = append(data, '\n')
		}
		data = append(data, bytes.TrimPrefix(line[5:], []byte(" "))...)
	}
	if err := scanner.Err(); err != nil {
		return false, gerror.Wrap(err, "read gemini subscription SSE")
	}
	return flush()
}
