package roundrobin

func Next[Entry comparable](entries []Entry, last Entry) Entry {
	if len(entries) == 0 {
		var zero Entry
		return zero
	}

	for i, entry := range entries {
		if entry == last {
			return entries[(i+1)%len(entries)]
		}
	}

	return entries[0]
}
