// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package gcc

import "time"

// MeasureRTT measures the RTT. TODO(ME): Remove this function from this
// package?
func MeasureRTT(reportSent, reportReceived, latestAckedSent, latestAckedArrival time.Time) time.Duration {
	pendingTime := reportSent.Sub(latestAckedArrival)

	return reportReceived.Sub(latestAckedSent) - pendingTime
}
