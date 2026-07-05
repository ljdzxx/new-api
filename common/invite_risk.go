package common

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
	InviteRiskWeights = weights
	return nil
}
