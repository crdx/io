package roundrobin

func NextIndex[Entry comparable](entries []Entry, last Entry, lastIndex int) int {
	if len(entries) == 0 {
		return -1
	}

	if lastIndex >= 0 && lastIndex < len(entries) && entries[lastIndex] == last {
		return (lastIndex + 1) % len(entries)
	}

	for i, entry := range entries {
		if entry == last {
			return (i + 1) % len(entries)
		}
	}

	return 0
}
