package service

import "testing"

func TestRetryParamFailedPriorityIsExcluded(t *testing.T) {
	param := &RetryParam{
		retryCandidates: []retryChannelCandidate{
			{ChannelID: 101, Priority: 10},
			{ChannelID: 102, Priority: 10},
			{ChannelID: 201, Priority: 5},
		},
	}

	param.MarkChannelFailed(101, 10)

	candidate, ok := param.nextRetryCandidate()
	if !ok {
		t.Fatal("expected a lower-priority retry candidate")
	}
	if candidate.ChannelID != 201 {
		t.Fatalf("expected retry to descend directly to channel 201, got %d", candidate.ChannelID)
	}
}
