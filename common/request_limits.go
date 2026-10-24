package common

import "math"

// MaxTokensLimit bounds token budgets before they enter pre-consume quota math.
// Shared by request validation and configuration that generates relay requests.
const MaxTokensLimit = math.MaxInt32 / 2
