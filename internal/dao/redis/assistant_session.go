package redis

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	assistantSessionKeyPrefix = "assistant_session:"
	assistantSessionTTL        = 24 * time.Hour
	assistantSessionMaxItems   = 20
)

type AssistantSessionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func assistantSessionKey(sessionID string) string {
	return fmt.Sprintf("%s%s", assistantSessionKeyPrefix, sessionID)
}

func GetAssistantSessionMessages(sessionID string) ([]AssistantSessionMessage, error) {
	if client == nil {
		return nil, fmt.Errorf("redis not initialized")
	}
	raw, err := client.Get(assistantSessionKey(sessionID)).Result()
	if err == Nil {
		return []AssistantSessionMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return []AssistantSessionMessage{}, nil
	}

	var messages []AssistantSessionMessage
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func SaveAssistantSessionMessages(sessionID string, messages []AssistantSessionMessage) error {
	if client == nil {
		return fmt.Errorf("redis not initialized")
	}
	if len(messages) > assistantSessionMaxItems {
		messages = messages[len(messages)-assistantSessionMaxItems:]
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	return client.Set(assistantSessionKey(sessionID), raw, assistantSessionTTL).Err()
}

