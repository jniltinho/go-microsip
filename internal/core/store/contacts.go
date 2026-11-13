package store

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/jniltinho/go-microsip/internal/core/domain"
)

// ContactNumber is one phone number of a contact.
type ContactNumber struct {
	// Number is a dial string, validated as a domain.DialTarget and stored trimmed.
	Number string
	// Label is optional, such as "work".
	Label string
}

// NewContact is a contact to insert or to replace one with.
type NewContact struct {
	Name string
	// Favorite contacts are listed first.
	Favorite bool
	// Numbers holds one to MaxNumbersPerContact distinct numbers.
	Numbers []ContactNumber
}

// Contact is a stored contact; its numbers are in insertion order.
type Contact struct {
	ID         int64
	Name       string
	Favorite   bool
	CreatedUTC int64
	Numbers    []ContactNumber
}

// ImportSummary is the result of ImportCSV.
type ImportSummary struct {
	// ContactsCreated counts the new contacts.
	ContactsCreated int
	// ContactsMerged counts the existing contacts (same exact name) that received numbers.
	ContactsMerged int
	// NumbersAdded counts the numbers added across all contacts.
	NumbersAdded int
}

func normalizeNumber(number string) (string, error) {
	target, err := domain.ParseDialTarget(number)
	if err != nil {
		return "", invalid(Number)
	}
	return target.Value(), nil
}

// validateContact checks a contact and returns it trimmed.
func validateContact(c NewContact) (NewContact, error) {
	out := NewContact{Name: strings.TrimSpace(c.Name), Favorite: c.Favorite}
	if !textOK(out.Name, MaxNameLen) {
		return out, invalid(Name)
	}
	if len(c.Numbers) == 0 || len(c.Numbers) > MaxNumbersPerContact {
		return out, invalid(NumberCount)
	}
	for _, n := range c.Numbers {
		number, err := normalizeNumber(n.Number)
		if err != nil {
			return out, err
		}
		for _, have := range out.Numbers {
			if have.Number == number {
				return out, invalid(DuplicateNumber)
			}
		}
		label := strings.TrimSpace(n.Label)
		if label != "" && !textOK(label, MaxLabelLen) {
			return out, invalid(Label)
		}
		out.Numbers = append(out.Numbers, ContactNumber{Number: number, Label: label})
	}
	return out, nil
}

// label stores an absent label as NULL, as the Rust application does.
func label(l string) any {
	if l == "" {
		return nil
	}
	return l
}

func insertNumbers(tx *sql.Tx, id int64, numbers []ContactNumber) error {
	for _, n := range numbers {
		if _, err := tx.Exec("INSERT INTO contact_numbers (contact_id, number, label) VALUES (?, ?, ?)", id, n.Number, label(n.Label)); err != nil {
			return err
		}
	}
	return nil
}

func insertContact(tx *sql.Tx, c NewContact) (int64, error) {
	res, err := tx.Exec("INSERT INTO contacts (name, favorite, created_utc) VALUES (?, ?, ?)", c.Name, c.Favorite, nowUTC())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, insertNumbers(tx, id, c.Numbers)
}

// AddContact inserts a contact with its numbers and returns the new ID. Nothing is written on an
// error.
func (s *Store) AddContact(c NewContact) (id int64, err error) {
	if c, err = validateContact(c); err != nil {
		return 0, err
	}
	err = s.tx(func(tx *sql.Tx) (err error) {
		id, err = insertContact(tx, c)
		return err
	})
	return id, err
}

// UpdateContact replaces a contact's name, favorite flag and numbers.
func (s *Store) UpdateContact(id int64, c NewContact) error {
	c, err := validateContact(c)
	if err != nil {
		return err
	}
	return s.tx(func(tx *sql.Tx) error {
		if err := found(changed(tx.Exec("UPDATE contacts SET name = ?, favorite = ? WHERE id = ?", c.Name, c.Favorite, id))); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM contact_numbers WHERE contact_id = ?", id); err != nil {
			return err
		}
		return insertNumbers(tx, id, c.Numbers)
	})
}

// DeleteContact deletes a contact and, by cascade, its numbers.
func (s *Store) DeleteContact(id int64) error {
	return found(changed(s.db.Exec("DELETE FROM contacts WHERE id = ?", id)))
}

// SetFavorite sets or clears the favorite flag.
func (s *Store) SetFavorite(id int64, favorite bool) error {
	return found(changed(s.db.Exec("UPDATE contacts SET favorite = ? WHERE id = ?", favorite, id)))
}

// Contact fetches one contact; ok is false when there is none with this ID.
func (s *Store) Contact(id int64) (c Contact, ok bool, err error) {
	err = s.db.QueryRow("SELECT id, name, favorite, created_utc FROM contacts WHERE id = ?", id).
		Scan(&c.ID, &c.Name, &c.Favorite, &c.CreatedUTC)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err == nil {
		c.Numbers, err = s.numbers(id)
	}
	return c, err == nil, wrap(err)
}

// SearchContacts lists the contacts whose name or any number contains query (case-insensitive; %
// and _ are literal), favorites first, then by name. An empty query lists all contacts.
func (s *Store) SearchContacts(query string, limit int) ([]Contact, error) {
	rows, err := s.db.Query(`SELECT id, name, favorite, created_utc FROM contacts c
		WHERE name LIKE ?1 ESCAPE '\'
		   OR EXISTS (SELECT 1 FROM contact_numbers n WHERE n.contact_id = c.id AND n.number LIKE ?1 ESCAPE '\')
		ORDER BY favorite DESC, name COLLATE NOCASE, id LIMIT ?2`, likePattern(strings.TrimSpace(query)), limit)
	if err != nil {
		return nil, wrap(err)
	}
	var out []Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.Name, &c.Favorite, &c.CreatedUTC); err != nil {
			rows.Close()
			return nil, wrap(err)
		}
		out = append(out, c)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, wrap(err)
	}
	// One numbers query per contact, bounded by limit; the one connection must be free first.
	for i := range out {
		if out[i].Numbers, err = s.numbers(out[i].ID); err != nil {
			return nil, wrap(err)
		}
	}
	return out, nil
}

func (s *Store) numbers(id int64) ([]ContactNumber, error) {
	rows, err := s.db.Query("SELECT number, COALESCE(label, '') FROM contact_numbers WHERE contact_id = ? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContactNumber
	for rows.Next() {
		var n ContactNumber
		if err := rows.Scan(&n.Number, &n.Label); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ImportCSV applies a clean CSV preview in one transaction: all or nothing.
//
// A preview contact whose exact name already exists is merged into the oldest such contact: only
// the numbers it does not have yet are added.
func (s *Store) ImportCSV(p *CSVPreview) (ImportSummary, error) {
	var summary ImportSummary
	if !p.Importable() {
		return summary, &Error{Kind: CSVInvalid}
	}
	contacts := make([]NewContact, 0, len(p.contacts))
	for _, c := range p.contacts {
		c, err := validateContact(c)
		if err != nil {
			return summary, err
		}
		contacts = append(contacts, c)
	}
	err := s.tx(func(tx *sql.Tx) error {
		for _, c := range contacts {
			var id int64
			err := tx.QueryRow("SELECT id FROM contacts WHERE name = ? ORDER BY id LIMIT 1", c.Name).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				if _, err := insertContact(tx, c); err != nil {
					return err
				}
				summary.ContactsCreated++
				summary.NumbersAdded += len(c.Numbers)
				continue
			}
			if err != nil {
				return err
			}
			added := 0
			for _, n := range c.Numbers {
				one, err := changed(tx.Exec("INSERT OR IGNORE INTO contact_numbers (contact_id, number, label) VALUES (?, ?, ?)", id, n.Number, label(n.Label)))
				if err != nil {
					return err
				}
				added += one
			}
			var total int
			if err := tx.QueryRow("SELECT COUNT(*) FROM contact_numbers WHERE contact_id = ?", id).Scan(&total); err != nil {
				return err
			}
			// The bound holds for merged contacts too; returning rolls the whole import back.
			if total > MaxNumbersPerContact {
				return invalid(NumberCount)
			}
			if added > 0 {
				summary.ContactsMerged++
				summary.NumbersAdded += added
			}
		}
		return nil
	})
	if err != nil {
		return ImportSummary{}, err
	}
	return summary, nil
}
