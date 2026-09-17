package ratio_setting

import "testing"

func TestGroupIconValidation(t *testing.T) {
	for _, value := range []string{
		`{}`, `{"codex-pro":"OpenAI","claude":"Claude.Color"}`,
	} {
		if err := CheckGroupIcon(value); err != nil {
			t.Errorf("valid config %s: %v", value, err)
		}
	}
	for _, value := range []string{
		`null`, `[]`, `{"":"OpenAI"}`, `{"  ":"OpenAI"}`,
		`{"test":null}`, `{"test":12}`, `{"test":""}`,
		`{"test":"OpenAI.size=20"}`, `{"test":"<svg>"}`, `{`,
	} {
		if err := CheckGroupIcon(value); err == nil {
			t.Errorf("accepted invalid config %s", value)
		}
	}
}

func TestGroupIconUpdate(t *testing.T) {
	original := GroupIcon2JSONString()
	t.Cleanup(func() { _ = UpdateGroupIconByJSONString(original) })
	if err := UpdateGroupIconByJSONString(`{"codex-pro":"OpenAI","auto":"Claude.Color"}`); err != nil {
		t.Fatal(err)
	}
	if GetGroupIcon("codex-pro") != "OpenAI" || GetGroupIcon("auto") != "Claude.Color" || GetGroupIcon("unknown") != "" {
		t.Fatal("unexpected group icon lookup")
	}
	if err := UpdateGroupIconByJSONString(`{"codex-pro":42}`); err == nil {
		t.Fatal("expected invalid update to fail")
	}
	if GetGroupIcon("codex-pro") != "OpenAI" {
		t.Fatal("invalid update changed existing configuration")
	}
	saved := GroupIcon2JSONString()
	if err := UpdateGroupIconByJSONString(`{}`); err != nil || GetGroupIcon("auto") != "" {
		t.Fatal("failed to clear configuration")
	}
	if err := UpdateGroupIconByJSONString(saved); err != nil || GetGroupIcon("codex-pro") != "OpenAI" {
		t.Fatal("failed to restore saved configuration")
	}
}
