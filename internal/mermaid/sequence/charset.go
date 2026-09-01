package sequence

type BoxChars struct {
	TopLeft      rune
	TopRight     rune
	BottomLeft   rune
	BottomRight  rune
	Horizontal   rune
	Vertical     rune
	TeeDown      rune
	TeeRight     rune
	TeeLeft      rune
	Cross        rune
	ArrowRight   rune
	ArrowLeft    rune
	CrossHead    rune
	PointRight   rune
	PointLeft    rune
	Circle       rune
	SolidLine    rune
	DottedLine   rune
	SelfTopRight rune
	SelfBottom   rune
}

var ASCII = BoxChars{
	TopLeft:      '+',
	TopRight:     '+',
	BottomLeft:   '+',
	BottomRight:  '+',
	Horizontal:   '-',
	Vertical:     '|',
	TeeDown:      '+',
	TeeRight:     '+',
	TeeLeft:      '+',
	Cross:        '+',
	ArrowRight:   '>',
	ArrowLeft:    '<',
	CrossHead:    'x',
	PointRight:   ')',
	PointLeft:    '(',
	Circle:       'o',
	SolidLine:    '-',
	DottedLine:   '.',
	SelfTopRight: '+',
	SelfBottom:   '+',
}

var Unicode = BoxChars{
	TopLeft:      '┌',
	TopRight:     '┐',
	BottomLeft:   '└',
	BottomRight:  '┘',
	Horizontal:   '─',
	Vertical:     '│',
	TeeDown:      '┬',
	TeeRight:     '├',
	TeeLeft:      '┤',
	Cross:        '┼',
	ArrowRight:   '►',
	ArrowLeft:    '◄',
	CrossHead:    '×',
	PointRight:   ')',
	PointLeft:    '(',
	Circle:       'o',
	SolidLine:    '─',
	DottedLine:   '┈',
	SelfTopRight: '┐',
	SelfBottom:   '┘',
}
