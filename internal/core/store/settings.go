package store

import (
	"database/sql"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Settings cross this boundary as strings keyed by the configuration's dotted keys
// (account.server, ui.theme, ...); the config package parses and validates them as its SQLite
// layer. Only values the user explicitly saved belong here, never the effective configuration with
// its command-line, environment or TOML overrides, and never a password: the account row holds at
// most a reference naming a keyring entry.

// ConfigKeys lists every settings key the store accepts; it mirrors the configuration schema.
var ConfigKeys = []string{
	"account.display_name", "account.username", "account.auth_username", "account.domain",
	"account.server", "account.port", "account.transport", "account.media", "account.expires",
	"account.password_env", "account.password_file", "account.ca_file",
	"ui.theme", "ui.dnd", "audio.input_device", "audio.output_device",
}

const (
	// MaxConfigValueLen bounds a settings value, in bytes.
	MaxConfigValueLen = 4096
	maxSecretRefLen   = 512
)

// sqlValue checks the key and converts the value to its SQL form; nil clears.
func sqlValue(key string, value *string) (any, error) {
	if !slices.Contains(ConfigKeys, key) {
		return nil, invalid(ConfigKey)
	}
	if value == nil {
		return nil, nil
	}
	if len(*value) > MaxConfigValueLen || strings.IndexFunc(*value, unicode.IsControl) >= 0 {
		return nil, invalid(ConfigValue)
	}
	if key == "account.port" || key == "account.expires" {
		n, err := strconv.ParseInt(*value, 10, 64)
		if err != nil {
			return nil, invalid(ConfigValue)
		}
		return n, nil
	}
	return *value, nil
}

// enabledAccount returns the ID of the enabled account, creating an empty one when there is none.
func enabledAccount(tx *sql.Tx) (int64, error) {
	var id int64
	err := tx.QueryRow("SELECT id FROM accounts WHERE enabled = 1").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := tx.Exec("INSERT INTO accounts (enabled, updated_utc) VALUES (1, ?)", nowUTC())
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	return id, err
}

// writeValues validates everything first, then writes; the caller owns the transaction.
func writeValues(tx *sql.Tx, changes map[string]*string) error {
	keys := make([]string, 0, len(changes))
	values := map[string]any{}
	for key, value := range changes {
		v, err := sqlValue(key, value)
		if err != nil {
			return err
		}
		keys, values[key] = append(keys, key), v
	}
	sort.Strings(keys)
	for _, key := range keys {
		var err error
		if column, account := strings.CutPrefix(key, "account."); account {
			var id int64
			if id, err = enabledAccount(tx); err == nil {
				// column comes from the ConfigKeys whitelist checked above.
				_, err = tx.Exec("UPDATE accounts SET "+column+" = ?, updated_utc = ? WHERE id = ?", values[key], nowUTC(), id)
			}
		} else if values[key] != nil {
			_, err = tx.Exec("INSERT INTO preferences (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", key, values[key])
		} else {
			_, err = tx.Exec("DELETE FROM preferences WHERE key = ?", key)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// LoadConfig returns the stored settings layer: every value that is set on the enabled account,
// and the stored preferences, keyed by ConfigKeys.
func (s *Store) LoadConfig() (map[string]string, error) {
	out := map[string]string{}
	var keys, columns []string
	for _, key := range ConfigKeys {
		if column, account := strings.CutPrefix(key, "account."); account {
			keys, columns = append(keys, key), append(columns, "CAST("+column+" AS TEXT)")
		}
	}
	values := make([]sql.NullString, len(keys))
	targets := make([]any, len(keys))
	for i := range values {
		targets[i] = &values[i]
	}
	err := s.db.QueryRow("SELECT " + strings.Join(columns, ", ") + " FROM accounts WHERE enabled = 1").Scan(targets...)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, wrap(err)
	}
	for i, v := range values {
		if v.Valid {
			out[keys[i]] = v.String
		}
	}
	rows, err := s.db.Query("SELECT key, value FROM preferences")
	if err != nil {
		return nil, wrap(err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, wrap(err)
		}
		if slices.Contains(ConfigKeys, key) {
			out[key] = value
		}
	}
	return out, wrap(rows.Err())
}

// SaveConfig persists the values the user explicitly edited; a nil value removes a stored one.
// Other stored values are kept. All or nothing.
//
// Pass only real user edits that no higher-precedence source overrides (config.SQLiteEdits), never
// the effective configuration.
func (s *Store) SaveConfig(changes map[string]*string) error {
	return s.tx(func(tx *sql.Tx) error { return writeValues(tx, changes) })
}

// ReplaceConfig replaces all stored settings with values in one transaction (a confirmed import).
// The account's secret reference is cleared too, because the imported account may be another one.
// On an error the previous settings remain.
func (s *Store) ReplaceConfig(values map[string]string) error {
	clear, set := map[string]*string{}, map[string]*string{}
	for _, key := range ConfigKeys {
		clear[key] = nil
	}
	for key, value := range values {
		set[key] = &value
	}
	return s.tx(func(tx *sql.Tx) error {
		if err := writeValues(tx, clear); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE accounts SET secret_ref = NULL WHERE enabled = 1"); err != nil {
			return err
		}
		return writeValues(tx, set)
	})
}

// SecretRef returns the keyring reference of the enabled account's password, or "" when none is
// stored.
func (s *Store) SecretRef() (string, error) {
	var ref sql.NullString
	err := s.db.QueryRow("SELECT secret_ref FROM accounts WHERE enabled = 1").Scan(&ref)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return ref.String, wrap(err)
}

// SetSecretRef stores the keyring reference, never the password itself; "" clears it.
func (s *Store) SetSecretRef(ref string) error {
	var value any
	if ref != "" {
		if !textOK(ref, maxSecretRefLen) {
			return invalid(SecretRef)
		}
		value = ref
	}
	return s.tx(func(tx *sql.Tx) error {
		id, err := enabledAccount(tx)
		if err == nil {
			_, err = tx.Exec("UPDATE accounts SET secret_ref = ?, updated_utc = ? WHERE id = ?", value, nowUTC(), id)
		}
		return err
	})
}
