// Package network monitors live upload and download bandwidth via Windows IP Helper APIs.
package network

import (
	"fmt"
	"math"
)

// FormattedSpeed splits a formatted rate into numeric value and unit parts
// so UI renderers can align both columns independently without horizontal jitter.
type FormattedSpeed struct {
	Value string
	Unit  string
}

// String returns the single-string representation (e.g. "12.4 MB/s").
func (f FormattedSpeed) String() string {
	return f.Value + " " + f.Unit
}

var (
	byteUnits = [4]string{"B/s", "KB/s", "MB/s", "GB/s"}
	bitUnits  = [4]string{"bps", "Kbps", "Mbps", "Gbps"}
)

// FormatSpeed scales bytesPerSec across B/s, KB/s, MB/s, and GB/s (binary 1024 steps).
func FormatSpeed(bytesPerSec float64) FormattedSpeed {
	return formatRate(bytesPerSec, 1024.0, byteUnits)
}

// FormatSpeedBits converts bytesPerSec into bits/sec and scales across bps, Kbps, Mbps, and Gbps
// using SI decimal 1000 steps matching ISP plans, speed tests, and Windows Task Manager.
func FormatSpeedBits(bytesPerSec float64) FormattedSpeed {
	return formatRate(bytesPerSec*8.0, 1000.0, bitUnits)
}

func formatRate(val, step float64, units [4]string) FormattedSpeed {
	if math.IsNaN(val) || math.IsInf(val, 0) || val <= 0 {
		return FormattedSpeed{Value: "0", Unit: units[0]}
	}

	unitIdx := 0
	for unitIdx < len(units)-1 && val >= step {
		val /= step
		unitIdx++
	}

	// Handle rounding at unit boundaries (e.g., 1023.9 B/s -> 1.00 KB/s or 999.9 bps -> 1.00 Kbps).
	if unitIdx == 0 {
		rounded := math.Round(val)
		if rounded >= step {
			val /= step
			unitIdx++
		} else {
			return FormattedSpeed{
				Value: fmt.Sprintf("%.0f", rounded),
				Unit:  units[0],
			}
		}
	}

	if unitIdx < len(units)-1 && math.Round(val) >= step {
		val /= step
		unitIdx++
	}

	var numStr string
	switch {
	case val < 9.995:
		numStr = fmt.Sprintf("%.2f", val)
	case val < 99.95:
		numStr = fmt.Sprintf("%.1f", val)
	default:
		numStr = fmt.Sprintf("%.0f", val)
	}

	return FormattedSpeed{
		Value: numStr,
		Unit:  units[unitIdx],
	}
}
