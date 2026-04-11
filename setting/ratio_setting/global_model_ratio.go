package ratio_setting

import (
	"math"
	"sync/atomic"
)

const DefaultGlobalModelRatio = 1.0

var globalModelRatioBits atomic.Uint64

func init() {
	globalModelRatioBits.Store(math.Float64bits(DefaultGlobalModelRatio))
}

func GetGlobalModelRatio() float64 {
	return math.Float64frombits(globalModelRatioBits.Load())
}

func SetGlobalModelRatio(ratio float64) {
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		ratio = DefaultGlobalModelRatio
	}
	if ratio < 0 {
		ratio = 0
	}
	globalModelRatioBits.Store(math.Float64bits(ratio))
}
