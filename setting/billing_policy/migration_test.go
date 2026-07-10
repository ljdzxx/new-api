package billing_policy

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestBuildLegacyMigrationCandidateVerifiesEveryPolicy(t *testing.T) {
	ratio_setting.InitRatioSettings()
	report, err := BuildLegacyMigrationCandidate(StateShadow)
	require.NoError(t, err, "%+v", report.Issues)
	require.NotEmpty(t, report.Candidate.Policies)
	require.Equal(t, report.Total, report.Verified)
	require.Equal(t, report.SourceChecksum, report.Candidate.Migration.SourceChecksum)
}
