package orchestrator

import (
	"encoding/json"
	"math"
	"strings"
	"sync"
)

type runMetrics struct {
	mu        sync.Mutex
	tokensIn  int
	tokensOut int
	costUSD   float64
	sessionID *string
}

func (m *runMetrics) observe(line string) {
	var value any
	if json.Unmarshal([]byte(line), &value) != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	walkMetrics(value, func(key string, number float64) {
		normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
		switch normalized {
		case "input_tokens", "tokens_in", "prompt_tokens":
			m.tokensIn = maxInt(m.tokensIn, int(math.Round(number)))
		case "output_tokens", "tokens_out", "completion_tokens":
			m.tokensOut = maxInt(m.tokensOut, int(math.Round(number)))
		case "cost_usd", "total_cost_usd":
			if number > m.costUSD {
				m.costUSD = number
			}
		}
	})
	if sessionID := findSessionID(value); sessionID != "" {
		m.sessionID = &sessionID
	}
}

func (m *runMetrics) cost() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.costUSD
}

func findSessionID(value any) string {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			if normalized == "session_id" || normalized == "sessionid" || normalized == "thread_id" {
				if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
					return strings.TrimSpace(text)
				}
			}
			if found := findSessionID(child); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range item {
			if found := findSessionID(child); found != "" {
				return found
			}
		}
	}
	return ""
}

func walkMetrics(value any, visit func(key string, number float64)) {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if number, ok := child.(float64); ok {
				visit(key, number)
			}
			walkMetrics(child, visit)
		}
	case []any:
		for _, child := range item {
			walkMetrics(child, visit)
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
