package escape

func GetEnd(runes []rune, start int) int {
	end := start + 1
	for end < len(runes) && runes[end] != 'm' && runes[end] != 'K' {
		end++
	}
	if end < len(runes) {
		end++
	}
	return end
}
