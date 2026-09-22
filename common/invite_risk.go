package common

import "fmt"

type InviteRiskScoreWeights struct {
	IP          int `json:"ip"`
	Fingerprint int `json:"fingerprint"`
	Canvas      int `json:"canvas"`
	WebGL       int `json:"webgl"`
	Audio       int `json:"audio"`
	Fonts       int `json:"fonts"`
	UA          int `json:"ua"`
	Locale      int `json:"locale"`
	Screen      int `json:"screen"`
	Hardware    int `json:"hardware"`
}

func DefaultInviteRiskScoreWeights() InviteRiskScoreWeights {
	return InviteRiskScoreWeights{
		IP:          25,
		Fingerprint: 30,
		Canvas:      10,
		WebGL:       10,
		Audio:       6,
		Fonts:       6,
		UA:          5,
		Locale:      4,
		Screen:      3,
		Hardware:    1,
	}
}

func (w InviteRiskScoreWeights) Total() int {
	return w.IP + w.Fingerprint + w.Canvas + w.WebGL + w.Audio + w.Fonts + w.UA + w.Locale + w.Screen + w.Hardware
}

func (w InviteRiskScoreWeights) Validate() error {
	for _, weight := range []int{w.IP, w.Fingerprint, w.Canvas, w.WebGL, w.Audio, w.Fonts, w.UA, w.Locale, w.Screen, w.Hardware} {
		if weight < 0 || weight > 100 {
			return fmt.Errorf("邀请奖励风控各项权重必须是 0 到 100 之间的整数")
		}
	}
	if w.Total() != 100 {
		return fmt.Errorf("邀请奖励风控权重总分必须等于 100")
	}
	return nil
}

func InviteRiskScoreWeights2JSONString() string {
	bytes, err := Marshal(InviteRiskWeights)
	if err != nil {
		return "{}"
	}
	return string(bytes)
}

func UpdateInviteRiskScoreWeightsByJSONString(value string) error {
	var weights InviteRiskScoreWeights
	if err := UnmarshalJsonStr(value, &weights); err != nil {
		return err
	}
	if err := weights.Validate(); err != nil {
		return err
	}
	InviteRiskWeights = weights
	return nil
}
