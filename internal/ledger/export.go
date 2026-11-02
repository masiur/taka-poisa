package ledger

import (
	"context"
	"encoding/csv"
	"fmt"
	"strings"
	"time"
)

// ExportKinds are the CSV files an account can download.
var ExportKinds = map[string]struct {
	header []string
	query  string
}{
	"contacts": {
		[]string{"id", "name", "email", "phone", "language", "notes", "created_at"},
		`SELECT id, name, coalesce(email, ''), coalesce(phone_e164, ''), locale, notes, created_at
		 FROM contacts WHERE account_id = $1 AND deleted_at IS NULL ORDER BY id`,
	},
	"agreements": {
		[]string{"id", "contact_id", "contact_name", "direction", "status", "currency", "principal", "started_on", "purpose", "created_at"},
		`SELECT a.id, a.contact_id, c.name, a.direction, a.status, a.currency, a.principal, a.started_on, a.purpose, a.created_at
		 FROM agreements a JOIN contacts c ON c.id = a.contact_id WHERE a.account_id = $1 ORDER BY a.id`,
	},
	"installments": {
		[]string{"agreement_id", "seq", "due_on", "amount"},
		`SELECT i.agreement_id, i.seq, i.due_on, i.amount
		 FROM installments i JOIN agreements a ON a.id = i.agreement_id WHERE a.account_id = $1 ORDER BY i.agreement_id, i.seq`,
	},
	"payments": {
		[]string{"id", "agreement_id", "amount", "paid_on", "method", "note", "source", "status", "created_at"},
		`SELECT p.id, p.agreement_id, p.amount, p.paid_on, p.method, p.note, p.source, p.status, p.created_at
		 FROM payments p JOIN agreements a ON a.id = p.agreement_id WHERE a.account_id = $1 ORDER BY p.id`,
	},
}

// Export writes one kind as CSV. Amounts are in minor units (poisa), as stored.
func (s *Store) Export(ctx context.Context, accountID int64, kind string, w *csv.Writer) error {
	spec, ok := ExportKinds[kind]
	if !ok {
		return ErrNotFound
	}
	rows, err := s.db.Query(ctx, spec.query, accountID)
	if err != nil {
		return err
	}
	defer rows.Close()
	if err := w.Write(spec.header); err != nil {
		return err
	}
	record := make([]string, len(spec.header))
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return err
		}
		for i, v := range values {
			record[i] = csvCell(v)
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func csvCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case time.Time:
		if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 && t.Location() == time.UTC {
			return t.Format("2006-01-02")
		}
		return t.UTC().Format(time.RFC3339)
	case string:
		// Cells starting with these run as formulas in spreadsheet apps (CSV injection).
		if t != "" && strings.ContainsRune("=+-@\t\r", rune(t[0])) {
			return "'" + t
		}
		return t
	default:
		return fmt.Sprint(t)
	}
}
