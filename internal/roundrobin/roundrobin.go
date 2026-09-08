package roundrobin

func NextIndex[Entry comparable](entries []Entry, last Entry, lastIndex int) int {
	return NextIndexWhere(entries, last, lastIndex, func(Entry) bool { return true })
}

func NextIndexWhere[Entry comparable](
	entries []Entry, last Entry, lastIndex int, isEligible func(Entry) bool,
) int {
	firstIndex := nextIndex(entries, last, lastIndex)
	if firstIndex < 0 {
		return -1
	}

	for offset := range entries {
		index := (firstIndex + offset) % len(entries)
		if isEligible(entries[index]) {
			return index
		}
	}

	return -1
}

func nextIndex[Entry comparable](entries []Entry, last Entry, lastIndex int) int {
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
