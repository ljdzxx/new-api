package vertex

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVertexMultiRegionURLs(t *testing.T) {
	for _, region := range []string{"us", "eu"} {
		for _, project := range []string{"", "test-project"} {
			for _, action := range []string{"generateContent", "streamGenerateContent?alt=sse"} {
				t.Run(region+"/"+project+"/"+action, func(t *testing.T) {
					path := "/v1"
					if project != "" {
						path += "/projects/" + project + "/locations/" + region
					}
					path += "/publishers/google/models/gemini-2.5-flash:" + action
					require.Equal(t, fmt.Sprintf("https://aiplatform.%s.rep.googleapis.com%s", region, path), BuildGoogleModelURL("", "v1", project, region, "gemini-2.5-flash", action))
					require.Equal(t, "https://gateway.example"+path, BuildGoogleModelURL("https://gateway.example/v1/", "v1", project, region, "gemini-2.5-flash", action))
				})
			}
		}
	}
}
