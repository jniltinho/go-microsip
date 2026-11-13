package store

import "strings"

const (
	// maxFields is the columns a CSV row may have: name,number[,label].
	maxFields = 3
	// CSVMaxBytes bounds the CSV input (10 MiB).
	CSVMaxBytes = 10 << 20
	// CSVMaxRecords bounds the data records of a CSV file, without the header.
	CSVMaxRecords = 100_000
)

// CSVIssueKind says why a CSV row was rejected.
type CSVIssueKind uint8

// Reasons a CSV row is rejected.
const (
	Malformed      CSVIssueKind = iota // broken quoting: a stray or unterminated quote
	ColumnCount                        // the field count differs from the header's
	InvalidName                        // empty, too long or with control characters
	InvalidNumber                      // not a valid dial target
	InvalidLabel                       // too long or with control characters
	TooManyNumbers                     // the contact would exceed the per-contact limit
)

// CSVIssue is one rejected row; Line is the 1-based line where the record starts.
type CSVIssue struct {
	Line int
	Kind CSVIssueKind
}

// CSVPreview is validated CSV content, ready for confirmation and Store.ImportCSV.
type CSVPreview struct {
	contacts   []NewContact
	records    int
	duplicates int
	issues     []CSVIssue
}

// Contacts are the contacts to import; rows with the same name are aggregated.
func (p *CSVPreview) Contacts() []NewContact { return p.contacts }

// RecordCount is the data records read, without the header and blank lines.
func (p *CSVPreview) RecordCount() int { return p.records }

// DuplicateCount is the exact duplicate rows (same name and number) that will be skipped.
func (p *CSVPreview) DuplicateCount() int { return p.duplicates }

// Issues are the rejected rows. Any issue blocks the whole import.
func (p *CSVPreview) Issues() []CSVIssue { return p.issues }

// WithoutIssues is the preview of the rows that were not rejected: what a user who has seen the
// rejected rows may still import.
func (p *CSVPreview) WithoutIssues() *CSVPreview {
	clean := *p
	clean.issues = nil
	return &clean
}

// String names the reason a row was rejected.
func (k CSVIssueKind) String() string {
	return [...]string{"broken quoting", "wrong number of columns", "invalid name", "invalid number", "invalid label", "too many numbers for one contact"}[k]
}

// Importable reports whether there are no issues and at least one contact.
func (p *CSVPreview) Importable() bool { return len(p.issues) == 0 && len(p.contacts) > 0 }

type csvRow struct {
	line      int
	fields    []string
	malformed bool
}

// PreviewCSV parses and validates CSV text (an RFC 4180 subset). A leading byte-order mark is
// ignored. The first non-blank record must be the header name,number or name,number,label, in any
// letter case. Row-level problems are reported in the preview's Issues, not as an error.
func PreviewCSV(input string) (*CSVPreview, error) {
	if len(input) > CSVMaxBytes {
		return nil, &Error{Kind: CSVTooLarge}
	}
	rows, err := parseCSV(strings.TrimPrefix(input, "\ufeff"))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].malformed {
		return nil, &Error{Kind: CSVHeader}
	}
	header := make([]string, len(rows[0].fields))
	for i, f := range rows[0].fields {
		header[i] = strings.ToLower(strings.TrimSpace(f))
	}
	columns := len(header)
	if columns < 2 || columns > 3 || header[0] != "name" || header[1] != "number" || columns == 3 && header[2] != "label" {
		return nil, &Error{Kind: CSVHeader}
	}

	p := &CSVPreview{}
	byName := map[string]int{}
	for _, row := range rows[1:] {
		p.records++
		issue := func(kind CSVIssueKind) { p.issues = append(p.issues, CSVIssue{Line: row.line, Kind: kind}) }
		if row.malformed {
			issue(Malformed)
			continue
		}
		if len(row.fields) != columns {
			issue(ColumnCount)
			continue
		}
		name := strings.TrimSpace(row.fields[0])
		if !textOK(name, MaxNameLen) {
			issue(InvalidName)
			continue
		}
		number, err := normalizeNumber(row.fields[1])
		if err != nil {
			issue(InvalidNumber)
			continue
		}
		label := ""
		if columns == 3 {
			label = strings.TrimSpace(row.fields[2])
		}
		if label != "" && !textOK(label, MaxLabelLen) {
			issue(InvalidLabel)
			continue
		}
		index, known := byName[name]
		if !known {
			index = len(p.contacts)
			byName[name] = index
			p.contacts = append(p.contacts, NewContact{Name: name})
		}
		c := &p.contacts[index]
		duplicate := false
		for _, n := range c.Numbers {
			duplicate = duplicate || n.Number == number
		}
		switch {
		case duplicate:
			p.duplicates++
		case len(c.Numbers) >= MaxNumbersPerContact:
			issue(TooManyNumbers)
		default:
			c.Numbers = append(c.Numbers, ContactNumber{Number: number, Label: label})
		}
	}
	return p, nil
}

// parseCSV splits the text into records, skipping blank lines, at most CSVMaxRecords plus the
// header.
func parseCSV(input string) ([]csvRow, error) {
	var rows []csvRow
	push := func(row csvRow) error {
		if blank := !row.malformed && len(row.fields) == 1 && row.fields[0] == ""; !blank {
			if len(rows) > CSVMaxRecords {
				return &Error{Kind: CSVTooManyRecords}
			}
			rows = append(rows, row)
		}
		return nil
	}
	line := 1
	row := csvRow{line: line}
	var field strings.Builder
	inQuotes, quoted, closed := false, false, false
	chars := []rune(input)
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		next := rune(-1)
		if i+1 < len(chars) {
			next = chars[i+1]
		}
		if inQuotes {
			switch {
			case c == '"' && next == '"':
				i++
				field.WriteRune('"')
			case c == '"':
				inQuotes, closed = false, true
			default:
				if c == '\n' {
					line++
				}
				field.WriteRune(c)
			}
			continue
		}
		switch {
		case c == ',':
			// At most name,number,label: an extra field marks the row malformed instead of
			// allocating one string per comma.
			if len(row.fields) >= maxFields {
				row.malformed = true
			} else {
				row.fields = append(row.fields, field.String())
			}
			field.Reset()
			quoted, closed = false, false
		case c == '\r' && next == '\n':
		case c == '\n':
			row.fields = append(row.fields, field.String())
			field.Reset()
			quoted, closed = false, false
			line++
			if err := push(row); err != nil {
				return nil, err
			}
			row = csvRow{line: line}
		case c == '"' && field.Len() == 0 && !quoted:
			inQuotes, quoted = true, true
		default:
			if c == '"' || closed {
				row.malformed = true
			}
			field.WriteRune(c)
		}
	}
	if inQuotes {
		row.malformed = true
	}
	if inQuotes || field.Len() > 0 || len(row.fields) > 0 || quoted {
		row.fields = append(row.fields, field.String())
		if err := push(row); err != nil {
			return nil, err
		}
	}
	return rows, nil
}
