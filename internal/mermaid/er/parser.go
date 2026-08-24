// Package er parses and renders mermaid entity-relationship (erDiagram) diagrams as ASCII: entity
// attribute tables connected by crow's-foot relationships.
package er

import (
	"fmt"
	"regexp"
	"strings"

	"crdx.org/io/internal/mermaid/diagram"
)

const erKeyword = "erDiagram"

// Cardinality is one end of a relationship (crow's-foot notation).
type Cardinality int

const (
	OnlyOne Cardinality = iota
	ZeroOrOne
	ZeroOrMore
	OneOrMore
)

// Attribute is one row of an entity's attribute table.
type Attribute struct {
	Type    string
	Name    string
	Keys    []string // PK, FK, UK
	Comment string
}

// Entity is a named box with an optional list of attributes. Name is the id used in relationships;
// Display is the label shown in the box (an alias if one was given, otherwise the name).
type Entity struct {
	Name       string
	Display    string
	Attributes []Attribute
}

// Relationship connects two entities with a cardinality at each end.
type Relationship struct {
	Left, Right string
	LeftCard    Cardinality
	RightCard   Cardinality
	Identifying bool // true for a solid (--) line, false for dashed (..)
	Label       string
}

// ErDiagram is a parsed entity-relationship diagram. Entities are kept in first -seen order; a
// relationship referencing an undeclared entity auto-creates it.
type ErDiagram struct {
	Entities      []*Entity
	Relationships []*Relationship
	byName        map[string]*Entity
}

var (
	cardAny = map[string]Cardinality{
		"||": OnlyOne,
		"|o": ZeroOrOne, "o|": ZeroOrOne,
		"}o": ZeroOrMore, "o{": ZeroOrMore,
		"}|": OneOrMore, "|{": OneOrMore,
		"1": OnlyOne, "only one": OnlyOne, "one": OnlyOne,
		"zero or one": ZeroOrOne, "one or zero": ZeroOrOne,
		"0+": ZeroOrMore, "zero or more": ZeroOrMore, "zero or many": ZeroOrMore,
		"many": ZeroOrMore, "many(0)": ZeroOrMore,
		"1+": OneOrMore, "one or more": OneOrMore, "one or many": OneOrMore, "many(1)": OneOrMore,
	}

	lineOpRegex = regexp.MustCompile(`[-.]{2}`)

	directionRegex = regexp.MustCompile(`(?i)^\s*direction\s+\S+\s*$`)

	styleLineRegex = regexp.MustCompile(`(?i)^\s*(classDef|class|style)\b`)

	accLineRegex = regexp.MustCompile(`(?i)^\s*(accTitle|accDescr)\s*[:{]`)

	entityHeaderRegex = regexp.MustCompile(`^\s*(?:"([^"]+)"|([^\s{}["]+))(?:\s*\[\s*"?([^"\]]+?)"?\s*\]|\s+(\S+))?\s*\{\s*$`)

	loneEntityRegex = regexp.MustCompile(`^\s*(?:"([^"]+)"|([^\s{}:|"\[]+))(?:\s*\[\s*"?([^"\]]+?)"?\s*\]|\s+(\S+))?\s*$`)

	attrKeyRegex = regexp.MustCompile(`^(?:PK|FK|UK)(?:\s*,\s*(?:PK|FK|UK))*$`)

	emptyBlockRegex = regexp.MustCompile(`\s*\{\s*\}\s*$`)

	classShorthandRegex = regexp.MustCompile(`:::[\w,-]+`)

	subgraphRegex = regexp.MustCompile(`^subgraph\b`)
)

// IsErDiagram reports whether the input's first meaningful line declares an erDiagram
// (case-insensitive, whole token).
func IsErDiagram(input string) bool {
	for line := range strings.SplitSeq(input, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "%%") {
			continue
		}
		low := strings.ToLower(t)
		return low == strings.ToLower(erKeyword) ||
			strings.HasPrefix(low, strings.ToLower(erKeyword)+" ")
	}
	return false
}

func (d *ErDiagram) entity(name string) *Entity {
	if e, ok := d.byName[name]; ok {
		return e
	}
	e := &Entity{Name: name, Display: name}
	d.byName[name] = e
	d.Entities = append(d.Entities, e)
	return e
}

// Parse parses an erDiagram into entities and relationships.
func Parse(input string) (*ErDiagram, error) {
	if !IsErDiagram(input) {
		return nil, fmt.Errorf("expected %q keyword", erKeyword)
	}
	lines := diagram.SplitLines(strings.TrimSpace(input))
	for i, l := range lines {
		lines[i] = stripComment(l)
	}

	d := &ErDiagram{byName: map[string]*Entity{}}

	seenKeyword := false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if !seenKeyword {
			seenKeyword = true
			continue
		}

		if accLineRegex.MatchString(line) {
			if strings.HasSuffix(line, "{") {
				for i++; i < len(lines) && !strings.Contains(lines[i], "}"); i++ {
				}
			}
			continue
		}
		if directionRegex.MatchString(line) {
			continue
		}

		if subgraphRegex.MatchString(line) || line == "end" {
			return nil, fmt.Errorf("line %d: er subgraphs are not supported", i+1)
		}

		if strings.Contains(line, ":::") {
			line = stripClassShorthand(line)
		}

		if emptyBlockRegex.MatchString(line) {
			line = strings.TrimSpace(emptyBlockRegex.ReplaceAllString(line, ""))
		}

		if m := entityHeaderRegex.FindStringSubmatch(line); m != nil {
			name := firstNonEmpty(m[1], m[2])
			e := d.entity(name)
			if alias := firstNonEmpty(m[3], m[4]); alias != "" {
				e.Display = alias
			}
			attrs, next, err := parseAttributeBlock(lines, i+1)
			if err != nil {
				return nil, fmt.Errorf("entity %q: %w", name, err)
			}
			e.Attributes = append(e.Attributes, attrs...)
			i = next
			continue
		}

		if d.parseRelationship(line) {
			continue
		}

		if styleLineRegex.MatchString(line) {
			continue
		}

		if m := loneEntityRegex.FindStringSubmatch(line); m != nil {
			e := d.entity(firstNonEmpty(m[1], m[2]))
			if alias := firstNonEmpty(m[3], m[4]); alias != "" {
				e.Display = alias
			}
			continue
		}

		return nil, fmt.Errorf("line %d: invalid syntax: %q", i+1, line)
	}

	return d, nil
}

func parseAttributeBlock(lines []string, start int) ([]Attribute, int, error) {
	var attrs []Attribute
	for i := start; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		closes := strings.HasSuffix(line, "}")
		if closes {
			line = strings.TrimSpace(strings.TrimSuffix(line, "}"))
		}
		if line != "" {
			attr, err := parseAttribute(line)
			if err != nil {
				return nil, i, fmt.Errorf("line %d: %w", i+1, err)
			}
			attrs = append(attrs, attr)
		}
		if closes {
			return attrs, i, nil
		}
	}
	return nil, len(lines), fmt.Errorf("unclosed attribute block (missing '}')")
}

func parseAttribute(line string) (Attribute, error) {
	comment := ""
	if idx := strings.Index(line, `"`); idx != -1 {
		if end := strings.LastIndex(line, `"`); end > idx {
			comment = line[idx+1 : end]
		} else {
			comment = line[idx+1:]
		}
		line = strings.TrimSpace(line[:idx])
	}
	fields := splitAttrTokens(line)
	if len(fields) < 2 {
		return Attribute{}, fmt.Errorf("attribute needs a type and name: %q", line)
	}
	attr := Attribute{
		Type:    strings.Trim(fields[0], "`"),
		Name:    strings.Trim(fields[1], "`"),
		Comment: comment,
	}
	if rest := strings.TrimSpace(strings.Join(fields[2:], " ")); rest != "" {
		if !attrKeyRegex.MatchString(rest) {
			return Attribute{}, fmt.Errorf("unexpected attribute tokens %q", rest)
		}
		for k := range strings.SplitSeq(rest, ",") {
			attr.Keys = append(attr.Keys, strings.TrimSpace(k))
		}
	}
	return attr, nil
}

func stripClassShorthand(line string) string {
	parts := strings.Split(line, `"`)
	for i := 0; i < len(parts); i += 2 {
		parts[i] = classShorthandRegex.ReplaceAllString(parts[i], "")
	}
	return strings.Join(parts, `"`)
}

func stripComment(line string) string {
	inQuote := false
	for i := range len(line) {
		switch {
		case line[i] == '"':
			inQuote = !inQuote
		case !inQuote && line[i] == '%' && i+1 < len(line) && line[i+1] == '%':
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return line
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (d *ErDiagram) parseRelationship(line string) bool {
	before, after, ok := strings.Cut(line, ":")
	if !ok {
		return false
	}
	main := strings.TrimSpace(before)
	label := strings.Join(strings.Fields(strings.Trim(strings.TrimSpace(after), `"`)), " ")

	var left, right string
	var identifying bool
	if loc := lineOpRegex.FindStringIndex(main); loc != nil {
		left = strings.TrimSpace(main[:loc[0]])
		right = strings.TrimSpace(main[loc[1]:])
		identifying = main[loc[0]:loc[1]] == "--"
	} else if i, w := findWordOp(main); i >= 0 {
		left = strings.TrimSpace(main[:i])
		right = strings.TrimSpace(main[i+len(w):])
		identifying = w == " to "
	} else {
		return false
	}

	e1, lcard := splitEntityCard(left, true)
	e2, rcard := splitEntityCard(right, false)
	lc, lok := cardAny[strings.ToLower(lcard)]
	rc, rok := cardAny[strings.ToLower(rcard)]
	if e1 == "" || e2 == "" || !lok || !rok {
		return false
	}
	d.entity(e1)
	d.entity(e2)
	d.Relationships = append(d.Relationships, &Relationship{
		Left: e1, Right: e2, LeftCard: lc, RightCard: rc,
		Identifying: identifying, Label: label,
	})
	return true
}

func findWordOp(s string) (int, string) {
	for _, w := range []string{" optionally to ", " to "} {
		if i := strings.Index(s, w); i >= 0 {
			return i, w
		}
	}
	return -1, ""
}

func splitEntityCard(part string, entityFirst bool) (string, string) {
	part = strings.TrimSpace(part)
	if entityFirst {
		if strings.HasPrefix(part, `"`) {
			if end := strings.Index(part[1:], `"`); end >= 0 {
				return part[1 : end+1], strings.TrimSpace(part[end+2:])
			}
		}
		toks := strings.Fields(part)
		if len(toks) == 0 {
			return "", ""
		}
		return strings.Trim(toks[0], `"`), strings.Join(toks[1:], " ")
	}
	if strings.HasSuffix(part, `"`) {
		if start := strings.LastIndex(part[:len(part)-1], `"`); start >= 0 {
			return part[start+1 : len(part)-1], strings.TrimSpace(part[:start])
		}
	}
	toks := strings.Fields(part)
	if len(toks) == 0 {
		return "", ""
	}
	return strings.Trim(toks[len(toks)-1], `"`), strings.Join(toks[:len(toks)-1], " ")
}

func splitAttrTokens(s string) []string {
	var toks []string
	var cur strings.Builder
	depth := 0
	inTick := false
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '`':
			inTick = !inTick
			cur.WriteRune(r)
		case r == '(' && !inTick:
			depth++
			cur.WriteRune(r)
		case r == ')' && !inTick:
			if depth > 0 {
				depth--
			}
			cur.WriteRune(r)
		case (r == ' ' || r == '\t') && depth == 0 && !inTick:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return toks
}
