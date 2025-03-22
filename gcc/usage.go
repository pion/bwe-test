// SPDX-FileCopyrightText: 2025 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package gcc

import "fmt"

type usage int

const (
	usageUnder  usage = -1
	usageNormal usage = 0
	usageOver   usage = 1
)

func (u usage) String() string {
	switch u {
	case usageOver:
		return "overuse"
	case usageUnder:
		return "underuse"
	case usageNormal:
		return "normal"
	default:
		return fmt.Sprintf("invalid usage: %d", u)
	}
}
