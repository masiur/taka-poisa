package ledger

import (
	"slices"

	"github.com/masiur/taka-poisa/internal/validate"
)

const (
	MaxInstallments = 120
	MaxAmount       = 1_000_000_000_000 // minor units
)

// ScheduleInput describes a repayment plan: generated from a count and frequency,
// or a custom list of dates and amounts.
type ScheduleInput struct {
	Mode      string             `json:"mode"`       // generate | custom
	Count     int                `json:"count"`      // generate: 1 = lump sum
	FirstDue  Date               `json:"first_due"`  // generate
	Frequency string             `json:"frequency"`  // generate: monthly | weekly | every_n_days
	EveryDays int                `json:"every_days"` // generate, every_n_days
	Items     []InstallmentInput `json:"items"`      // custom
}

type InstallmentInput struct {
	DueOn  Date  `json:"due_on"`
	Amount int64 `json:"amount"`
}

// Build turns the input into installments that sum exactly to total, ordered by due date.
// Generated plans split evenly and put the remainder on the last installment.
func (s ScheduleInput) Build(total int64, errs validate.Errors) []InstallmentInput {
	switch s.Mode {
	case "generate":
		return s.generate(total, errs)
	case "custom":
		return s.custom(total, errs)
	default:
		errs.Add("schedule.mode", validate.Invalid)
		return nil
	}
}

func (s ScheduleInput) generate(total int64, errs validate.Errors) []InstallmentInput {
	if s.Count < 1 || s.Count > MaxInstallments {
		errs.Add("schedule.count", validate.Invalid)
	}
	if !saneDate(s.FirstDue) {
		errs.Add("schedule.first_due", validate.Invalid)
	}
	if s.Count > 1 {
		switch s.Frequency {
		case "monthly", "weekly":
		case "every_n_days":
			if s.EveryDays < 1 || s.EveryDays > 365 {
				errs.Add("schedule.every_days", validate.Invalid)
			}
		default:
			errs.Add("schedule.frequency", validate.Invalid)
		}
	}
	if total > 0 && int64(s.Count) > total {
		errs.Add("schedule.count", validate.Invalid) // every installment must be at least 1 minor unit
	}
	if len(errs) > 0 {
		return nil
	}

	base := total / int64(s.Count)
	out := make([]InstallmentInput, s.Count)
	for i := range out {
		var due Date
		switch s.Frequency {
		case "weekly":
			due = s.FirstDue.AddDays(7 * i)
		case "every_n_days":
			due = s.FirstDue.AddDays(s.EveryDays * i)
		default: // monthly, or a lump sum
			due = s.FirstDue.AddMonthsClamped(i, s.FirstDue.Time().Day())
		}
		out[i] = InstallmentInput{DueOn: due, Amount: base}
	}
	out[len(out)-1].Amount = total - base*int64(s.Count-1)
	return out
}

func (s ScheduleInput) custom(total int64, errs validate.Errors) []InstallmentInput {
	if len(s.Items) < 1 || len(s.Items) > MaxInstallments {
		errs.Add("schedule.items", validate.Invalid)
		return nil
	}
	var sum int64
	for _, it := range s.Items {
		if it.Amount <= 0 || it.Amount > MaxAmount || !saneDate(it.DueOn) {
			errs.Add("schedule.items", validate.Invalid)
			return nil
		}
		sum += it.Amount
	}
	if sum != total {
		errs.Add("schedule.items", "sum_mismatch")
		return nil
	}
	out := slices.Clone(s.Items)
	slices.SortStableFunc(out, func(a, b InstallmentInput) int { return a.DueOn.Time().Compare(b.DueOn.Time()) })
	return out
}

// saneDate rejects missing dates and typos like year 20256.
func saneDate(d Date) bool {
	return !d.IsZero() && d.Year() >= 2000 && d.Year() <= 2100
}

// InstallmentState is an installment with the confirmed payments applied to it.
type InstallmentState struct {
	ID        int64  `json:"id"`
	Seq       int    `json:"seq"`
	DueOn     Date   `json:"due_on"`
	Amount    int64  `json:"amount"`
	Paid      int64  `json:"paid"`
	Remaining int64  `json:"remaining"`
	Status    string `json:"status"` // paid | partial | overdue | due_today | upcoming
}

// Allocate applies paid to installments oldest first (by seq) and labels each one
// relative to today. The SQL summaries in store.go use the same rule:
// an installment's remaining amount is clamp(cumulative amount - paid, 0, amount).
func Allocate(items []InstallmentState, paid int64, today Date) []InstallmentState {
	out := slices.Clone(items)
	slices.SortFunc(out, func(a, b InstallmentState) int { return a.Seq - b.Seq })
	left := paid
	for i := range out {
		it := &out[i]
		it.Paid = min(max(left, 0), it.Amount)
		left -= it.Paid
		it.Remaining = it.Amount - it.Paid
		switch {
		case it.Remaining == 0:
			it.Status = "paid"
		case it.DueOn.Before(today):
			it.Status = "overdue"
		case it.DueOn == today:
			it.Status = "due_today"
		case it.Paid > 0:
			it.Status = "partial"
		default:
			it.Status = "upcoming"
		}
	}
	return out
}
