// Package ifaces is the one list of the interfaces the addon records: the radio interfaces whose
// devices run on batteries. BidCos-Wired devices are mains-powered and VirtualDevices are the
// system's groups, not devices with a battery - they are never listed, seeded, subscribed to or
// shown. Devices of another interface that an older version recorded stay in the database
// (retired, never deleted) and are left out of every list.
package ifaces

import "strings"

// Supported are the interfaces the addon records.
var Supported = []string{"BidCos-RF", "HmIP-RF"}

// IsSupported says whether the addon records the interface's devices.
func IsSupported(name string) bool {
	for _, s := range Supported {
		if s == name {
			return true
		}
	}
	return false
}

// SQL answers the condition "<column> IN ('BidCos-RF', 'HmIP-RF')" for a query (the names are
// constants of this package, never user input).
func SQL(column string) string {
	q := make([]string, len(Supported))
	for i, s := range Supported {
		q[i] = "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return column + " IN (" + strings.Join(q, ", ") + ")"
}
