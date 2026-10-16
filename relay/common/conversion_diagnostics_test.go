package common

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConversionDiagnosticsAreBoundedDetachedAndReset(t *testing.T) {
	info := &RelayInfo{}
	d := types.ConversionDiagnostic{Code: "test", Path: "tools", Severity: types.ConversionDiagnosticWarning}
	info.RecordConversionDiagnostics(nil, []types.ConversionDiagnostic{d, d})
	require.Len(t, info.ConversionDiagnostics(), 1)
	copy := info.ConversionDiagnostics()
	copy[0].Code = "mutated"
	require.Equal(t, "test", info.ConversionDiagnostics()[0].Code)
	for i := 0; i < 40; i++ {
		d.Path = fmt.Sprintf("tools[%d]", i)
		info.RecordConversionDiagnostics(nil, []types.ConversionDiagnostic{d})
	}
	require.Len(t, info.ConversionDiagnostics(), 32)
	require.True(t, info.ConversionDiagnosticsTruncated())
	info.ResetConversionDiagnostics()
	require.Empty(t, info.ConversionDiagnostics())
	require.False(t, info.ConversionDiagnosticsTruncated())
}

func TestConversionDiagnosticsKeepFieldsWithoutRepeatingConsoleWarnings(t *testing.T) {
	var logs bytes.Buffer
	old := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logs
	t.Cleanup(func() { gin.DefaultErrorWriter = old })
	info := &RelayInfo{}
	for i := 0; i < 30; i++ {
		info.RecordConversionDiagnostics(nil, []types.ConversionDiagnostic{{Code: "unsupported_function_strict", Path: fmt.Sprintf("tools[%d].strict", i), Severity: types.ConversionDiagnosticWarning}})
	}
	info.RecordConversionDiagnostics(nil, []types.ConversionDiagnostic{{Code: "unsupported_search_constraints", Path: "tools[30]", Severity: types.ConversionDiagnosticError}})
	require.Len(t, info.ConversionDiagnostics(), 31)
	require.Equal(t, 1, bytes.Count(logs.Bytes(), []byte("unsupported_function_strict")))
	require.Contains(t, logs.String(), "unsupported_search_constraints")
}
