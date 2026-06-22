package model

import "testing"

func TestMaskLotteryUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		want     string
	}{
		{name: "empty", username: "", want: ""},
		{name: "single character", username: "a", want: "a"},
		{name: "two characters", username: "ab", want: "a*"},
		{name: "three characters", username: "abc", want: "a*c"},
		{name: "longer username", username: "alice", want: "a***e"},
		{name: "unicode username", username: "张三丰", want: "张*丰"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskLotteryUsername(tt.username); got != tt.want {
				t.Fatalf("MaskLotteryUsername(%q) = %q, want %q", tt.username, got, tt.want)
			}
		})
	}
}
