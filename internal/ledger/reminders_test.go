package ledger

import (
	"testing"
	"time"
)

func ptr(v int) *int { return &v }

func TestPlanSends(t *testing.T) {
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, dhaka) // 10:00 on Nov 2
	items := []InstallmentState{
		{Seq: 1, DueOn: d("2026-11-02"), Amount: 100, Remaining: 100},
		{Seq: 2, DueOn: d("2026-11-05"), Amount: 100, Remaining: 100},
		{Seq: 3, DueOn: d("2026-12-02"), Amount: 100, Remaining: 0}, // paid: never reminded
	}
	rules := []Rule{
		{ID: 1, OffsetDays: 0, Recipient: "contact", Tone: "polite"},                                            // on the day
		{ID: 2, OffsetDays: 3, Recipient: "contact", Tone: "firm"},                                              // 3 days late
		{ID: 3, OffsetDays: -3, Recipient: "contact", Tone: "polite", SendHour: ptr(18)},                        // 3 days before, 18:00
		{ID: 4, OffsetDays: 1, Recipient: "self", Tone: "polite", RepeatEveryDays: ptr(2), RepeatCount: ptr(1)}, // owner: day after, then +2
		{ID: 5, OffsetDays: 10, Recipient: "contact", Tone: "polite", Paused: true},
	}
	got := PlanSends(items, rules, now, dhaka, 9, map[string]bool{"contact": true, "self": true})

	type row struct {
		who, day, tone string
		hour           int
	}
	var rows []row
	for _, p := range got {
		rows = append(rows, row{p.Recipient, p.Day.String(), p.Tone, p.At.In(dhaka).Hour()})
	}
	want := []row{
		// Nov 2 09:00 (installment 1 due) is already past at 10:00, so it is not planned.
		// Installment 1 +3 (Nov 5, firm) collides with installment 2 due (Nov 5, polite): firm wins.
		{"self", "2026-11-03", "polite", 9},  // inst 1 +1
		{"contact", "2026-11-05", "firm", 9}, // collapsed
		{"self", "2026-11-05", "polite", 9},  // inst 1 +1 repeat (+2)
		{"self", "2026-11-06", "polite", 9},  // inst 2 +1
		{"contact", "2026-11-08", "firm", 9}, // inst 2 +3
		{"self", "2026-11-08", "polite", 9},  // inst 2 +1 repeat
	}
	// Installment 2's "3 days before" (Nov 2 18:00) is still ahead of now.
	want = append([]row{{"contact", "2026-11-02", "polite", 18}}, want...)
	if len(rows) != len(want) {
		t.Fatalf("got %d sends %v, want %d %v", len(rows), rows, len(want), want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("send %d: got %+v, want %+v", i, rows[i], want[i])
		}
	}
}

func TestPlanSendsSkipsIneligibleAndCaps(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []InstallmentState{{Seq: 1, DueOn: d("2026-02-01"), Amount: 1, Remaining: 1}}
	rules := []Rule{{ID: 1, Recipient: "contact", Tone: "polite"}}
	if got := PlanSends(items, rules, now, time.UTC, 9, map[string]bool{"self": true}); len(got) != 0 {
		t.Fatalf("ineligible contact planned: %v", got)
	}

	var many []InstallmentState
	for i := range 120 {
		many = append(many, InstallmentState{Seq: i + 1, DueOn: d("2026-02-01").AddDays(i * 3), Amount: 1, Remaining: 1})
	}
	rules = []Rule{
		{ID: 1, OffsetDays: 0, Recipient: "contact", Tone: "polite", RepeatEveryDays: ptr(1), RepeatCount: ptr(20)},
		{ID: 2, OffsetDays: 0, Recipient: "self", Tone: "polite", RepeatEveryDays: ptr(1), RepeatCount: ptr(20)},
	}
	got := PlanSends(many, rules, now, time.UTC, 9, map[string]bool{"contact": true, "self": true})
	if len(got) != maxPlannedPerLoan {
		t.Fatalf("planned %d, want cap %d", len(got), maxPlannedPerLoan)
	}
	seen := map[string]bool{}
	for _, p := range got {
		k := p.Recipient + p.Day.String()
		if seen[k] {
			t.Fatalf("two sends for %s", k)
		}
		seen[k] = true
	}
}

func TestValidateRules(t *testing.T) {
	ok := []Rule{{OffsetDays: -3, Recipient: "contact", Tone: "polite", RepeatEveryDays: ptr(3), RepeatCount: ptr(5), SendHour: ptr(20)}}
	if errs := ValidateRules(ok); len(errs) > 0 {
		t.Fatalf("valid rules rejected: %v", errs)
	}
	bad := [][]Rule{
		{{OffsetDays: 61, Recipient: "contact", Tone: "polite"}},
		{{Recipient: "boss", Tone: "polite"}},
		{{Recipient: "self", Tone: "angry"}},
		{{Recipient: "self", Tone: "polite", RepeatEveryDays: ptr(2)}}, // repeat without count
		{{Recipient: "self", Tone: "polite", SendHour: ptr(24)}},
		make([]Rule, MaxRules+1),
	}
	for i, r := range bad {
		if errs := ValidateRules(r); errs["rules"] == "" {
			t.Errorf("case %d accepted", i)
		}
	}
}
