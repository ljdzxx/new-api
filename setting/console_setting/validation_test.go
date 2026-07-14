package console_setting

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateAnnouncementsAllowsEmbeddedHTML(t *testing.T) {
	content := `<section style="padding: 16px"><h2>Maintenance</h2><p>Service is available.</p></section>`
	settings := fmt.Sprintf(
		`[{"content":%q,"publishDate":"2026-07-14T12:00:00Z","type":"default"}]`,
		content,
	)

	if err := validateAnnouncements(settings); err != nil {
		t.Fatalf("expected embedded HTML to be accepted, got %v", err)
	}
}

func TestValidateAnnouncementsContentLength(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "at limit", content: strings.Repeat("公", maxAnnouncementContentLength)},
		{name: "over limit", content: strings.Repeat("a", maxAnnouncementContentLength+1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := fmt.Sprintf(
				`[{"content":%q,"publishDate":"2026-07-14T12:00:00Z","type":"default"}]`,
				tt.content,
			)
			err := validateAnnouncements(settings)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateAnnouncements() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
