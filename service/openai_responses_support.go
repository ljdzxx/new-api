package service

import "fmt"

func UnsupportedOpenAIResponsesProtocolError(channelID int) error {
	return fmt.Errorf("渠道 #%d 暂不支持走OpenAI /v1/responses 协议", channelID)
}
