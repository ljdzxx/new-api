package common

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const maxConversionDiagnostics = 32

// Diagnostics are per attempt. Stream writers are serialized by the scanner;
// settlement reads them after scanner cleanup.
func (info *RelayInfo) ResetConversionDiagnostics() {
	if info == nil {
		return
	}
	info.conversionDiagnostics = nil
	info.conversionDiagnosticsTruncated = false
}

func (info *RelayInfo) RecordConversionDiagnostics(c *gin.Context, diagnostics []types.ConversionDiagnostic) {
	if info == nil {
		return
	}
	// Avoid wrapping a typed nil *gin.Context in context.Context.
	var ctx context.Context = context.Background()
	if c != nil {
		ctx = c
	}
	for _, diagnostic := range diagnostics {
		duplicate := false
		codeLogged := false
		for _, previous := range info.conversionDiagnostics {
			if previous.Code == diagnostic.Code && previous.Severity == diagnostic.Severity && previous.From == diagnostic.From && previous.To == diagnostic.To {
				codeLogged = true
			}
			if previous.Code == diagnostic.Code && previous.Path == diagnostic.Path && previous.Severity == diagnostic.Severity && previous.From == diagnostic.From && previous.To == diagnostic.To {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if len(info.conversionDiagnostics) >= maxConversionDiagnostics {
			if !info.conversionDiagnosticsTruncated {
				logger.LogWarn(ctx, "conversion diagnostics truncated after 32 distinct entries")
			}
			info.conversionDiagnosticsTruncated = true
			continue
		}
		info.conversionDiagnostics = append(info.conversionDiagnostics, diagnostic)
		// Keep per-field diagnostics for the admin log, but avoid one identical
		// console warning per Codex function declaration (often dozens).
		if !codeLogged {
			logger.LogWarn(ctx, fmt.Sprintf("conversion diagnostic: code=%q severity=%q from=%q to=%q path=%q message=%q", diagnostic.Code, diagnostic.Severity, diagnostic.From, diagnostic.To, diagnostic.Path, diagnostic.Message))
		}
	}
}

func (info *RelayInfo) ConversionDiagnostics() []types.ConversionDiagnostic {
	if info == nil {
		return nil
	}
	return append([]types.ConversionDiagnostic(nil), info.conversionDiagnostics...)
}

func (info *RelayInfo) ConversionDiagnosticsTruncated() bool {
	return info != nil && info.conversionDiagnosticsTruncated
}
