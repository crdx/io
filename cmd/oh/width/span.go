package width

// —————————————————————————————————————————————————————————————————————————————————————————————————
// mega:allow-file comment-inlines
// —————————————————————————————————————————————————————————————————————————————————————————————————

type span struct {
	first rune
	last  rune
}

var spans = []span{
	{0x1100, 0x115F},   // hangul jamo, initial consonants
	{0x231A, 0x231B},   // watch, hourglass
	{0x23E9, 0x23EC},   // the double arrows
	{0x23F0, 0x23F0},   // alarm clock
	{0x23F3, 0x23F3},   // hourglass flowing
	{0x25FD, 0x25FE},   // medium small squares
	{0x2614, 0x2615},   // umbrella, hot beverage
	{0x2648, 0x2653},   // the zodiac
	{0x267F, 0x267F},   // wheelchair
	{0x2693, 0x2693},   // anchor
	{0x26A1, 0x26A1},   // high voltage
	{0x26AA, 0x26AB},   // medium circles
	{0x26BD, 0x26BE},   // football, baseball
	{0x26C4, 0x26C5},   // snowman, sun behind cloud
	{0x26CE, 0x26CE},   // ophiuchus
	{0x26D4, 0x26D4},   // no entry
	{0x26EA, 0x26EA},   // church
	{0x26F2, 0x26F3},   // fountain, golf
	{0x26F5, 0x26F5},   // sailboat
	{0x26FA, 0x26FA},   // tent
	{0x26FD, 0x26FD},   // fuel pump
	{0x2705, 0x2705},   // check mark
	{0x270A, 0x270B},   // raised fist, raised hand
	{0x2728, 0x2728},   // sparkles
	{0x274C, 0x274C},   // cross mark
	{0x274E, 0x274E},   // squared cross mark
	{0x2753, 0x2755},   // the question and exclamation marks
	{0x2757, 0x2757},   // exclamation mark
	{0x2795, 0x2797},   // plus, minus, division
	{0x27B0, 0x27B0},   // curly loop
	{0x27BF, 0x27BF},   // double curly loop
	{0x2B1B, 0x2B1C},   // large squares
	{0x2B50, 0x2B50},   // star
	{0x2B55, 0x2B55},   // large circle
	{0x2E80, 0x303E},   // cjk radicals through to the cjk symbols
	{0x3041, 0x33FF},   // hiragana through to the cjk compatibility forms
	{0x3400, 0x4DBF},   // cjk unified ideographs extension a
	{0x4E00, 0x9FFF},   // cjk unified ideographs
	{0xA000, 0xA4CF},   // yi syllables
	{0xA960, 0xA97F},   // hangul jamo extended a
	{0xAC00, 0xD7A3},   // hangul syllables
	{0xF900, 0xFAFF},   // cjk compatibility ideographs
	{0xFE10, 0xFE19},   // vertical forms
	{0xFE30, 0xFE6F},   // cjk compatibility forms, small form variants
	{0xFF00, 0xFF60},   // fullwidth forms
	{0xFFE0, 0xFFE6},   // fullwidth signs
	{0x16FE0, 0x16FFF}, // tangut, and the ideographic symbols with it
	{0x17000, 0x18AFF}, // tangut ideographs
	{0x1B000, 0x1B2FF}, // kana supplement
	{0x1F004, 0x1F004}, // mahjong red dragon
	{0x1F0CF, 0x1F0CF}, // joker
	{0x1F18E, 0x1F18E}, // negative squared ab
	{0x1F191, 0x1F19A}, // the squared words
	{0x1F200, 0x1F2FF}, // enclosed ideographic supplement
	{0x1F300, 0x1F64F}, // miscellaneous symbols and pictographs, and the emoticons
	{0x1F680, 0x1F6FF}, // transport and map symbols
	{0x1F7E0, 0x1F7F0}, // the coloured circles and squares
	{0x1F900, 0x1F9FF}, // supplemental symbols and pictographs
	{0x1FA70, 0x1FAFF}, // symbols and pictographs extended a
	{0x20000, 0x2FFFD}, // cjk unified ideographs extension b onwards
	{0x30000, 0x3FFFD}, // cjk unified ideographs extension g onwards
}
