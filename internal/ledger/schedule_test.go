package ledger

import (
	"testing"
	"time"

	"github.com/masiur/taka-poisa/internal/validate"
)

func d(s string) Date {
	v, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return v
}

func TestGenerateMonthlyClampsMonthEnd(t *testing.T) {
	errs := validate.Errors{}
	got := ScheduleInput{Mode: "generate", Count: 4, FirstDue: d("2027-01-31"), Frequency: "monthly"}.Build(1_000_00, errs)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	want := []string{"2027-01-31", "2027-02-28", "2027-03-31", "2027-04-30"}
	for i, w := range want {
		if got[i].DueOn.String() != w {
			t.Errorf("installment %d due %s, want %s", i+1, got[i].DueOn, w)
		}
	}
	// Leap year keeps the 29th.
	got = ScheduleInput{Mode: "generate", Count: 2, FirstDue: d("2028-01-30"), Frequency: "monthly"}.Build(100, validate.Errors{})
	if got[1].DueOn.String() != "2028-02-29" {
		t.Errorf("leap February: %s", got[1].DueOn)
	}
}

func TestGenerateSplitsRemainderOnLast(t *testing.T) {
	got := ScheduleInput{Mode: "generate", Count: 3, FirstDue: d("2026-12-02"), Frequency: "monthly"}.Build(1000_01, validate.Errors{})
	if got[0].Amount != 333_33 || got[1].Amount != 333_33 || got[2].Amount != 333_35 {
		t.Fatalf("amounts %d %d %d", got[0].Amount, got[1].Amount, got[2].Amount)
	}
	var sum int64
	for _, it := range got {
		sum += it.Amount
	}
	if sum != 1000_01 {
		t.Fatalf("sum %d", sum)
	}
}

func TestGenerateWeeklyAndEveryNDays(t *testing.T) {
	w := ScheduleInput{Mode: "generate", Count: 3, FirstDue: d("2026-11-02"), Frequency: "weekly"}.Build(300, validate.Errors{})
	if w[2].DueOn.String() != "2026-11-16" {
		t.Errorf("weekly third: %s", w[2].DueOn)
	}
	n := ScheduleInput{Mode: "generate", Count: 2, FirstDue: d("2026-11-02"), Frequency: "every_n_days", EveryDays: 10}.Build(300, validate.Errors{})
	if n[1].DueOn.String() != "2026-11-12" {
		t.Errorf("every 10 days second: %s", n[1].DueOn)
	}
}

func TestLumpSumNeedsNoFrequency(t *testing.T) {
	errs := validate.Errors{}
	got := ScheduleInput{Mode: "generate", Count: 1, FirstDue: d("2027-02-02")}.Build(30000_00, errs)
	if len(errs) > 0 || len(got) != 1 || got[0].Amount != 30000_00 {
		t.Fatalf("lump sum: %v %v", got, errs)
	}
}

func TestScheduleValidation(t *testing.T) {
	cases := map[string]struct {
		in    ScheduleInput
		total int64
		field string
	}{
		"bad mode":        {ScheduleInput{Mode: "x"}, 100, "schedule.mode"},
		"zero count":      {ScheduleInput{Mode: "generate", Count: 0, FirstDue: d("2027-01-01"), Frequency: "monthly"}, 100, "schedule.count"},
		"too many":        {ScheduleInput{Mode: "generate", Count: 121, FirstDue: d("2027-01-01"), Frequency: "monthly"}, 100000, "schedule.count"},
		"more than total": {ScheduleInput{Mode: "generate", Count: 5, FirstDue: d("2027-01-01"), Frequency: "monthly"}, 3, "schedule.count"},
		"no first due":    {ScheduleInput{Mode: "generate", Count: 1}, 100, "schedule.first_due"},
		"year typo":       {ScheduleInput{Mode: "generate", Count: 1, FirstDue: NewDate(2207, 1, 1)}, 100, "schedule.first_due"},
		"no frequency":    {ScheduleInput{Mode: "generate", Count: 2, FirstDue: d("2027-01-01")}, 100, "schedule.frequency"},
		"every 0 days":    {ScheduleInput{Mode: "generate", Count: 2, FirstDue: d("2027-01-01"), Frequency: "every_n_days"}, 100, "schedule.every_days"},
		"custom sum":      {ScheduleInput{Mode: "custom", Items: []InstallmentInput{{d("2027-01-01"), 40}, {d("2027-02-01"), 50}}}, 100, "schedule.items"},
		"custom empty":    {ScheduleInput{Mode: "custom"}, 100, "schedule.items"},
		"custom zero":     {ScheduleInput{Mode: "custom", Items: []InstallmentInput{{d("2027-01-01"), 0}}}, 0, "schedule.items"},
	}
	for name, c := range cases {
		errs := validate.Errors{}
		c.in.Build(c.total, errs)
		if errs[c.field] == "" {
			t.Errorf("%s: want error on %s, got %v", name, c.field, errs)
		}
	}
}

func TestCustomScheduleSortsByDate(t *testing.T) {
	errs := validate.Errors{}
	got := ScheduleInput{Mode: "custom", Items: []InstallmentInput{{d("2027-03-01"), 60}, {d("2027-01-01"), 40}}}.Build(100, errs)
	if len(errs) > 0 || got[0].DueOn.String() != "2027-01-01" {
		t.Fatalf("custom: %v %v", got, errs)
	}
}

func TestAllocateOldestFirst(t *testing.T) {
	items := []InstallmentState{
		{Seq: 2, DueOn: d("2026-12-02"), Amount: 100},
		{Seq: 1, DueOn: d("2026-11-02"), Amount: 100},
		{Seq: 3, DueOn: d("2027-01-02"), Amount: 100},
	}
	got := Allocate(items, 150, d("2026-12-02"))
	want := []struct {
		paid      int64
		remaining int64
		status    string
	}{{100, 0, "paid"}, {50, 50, "due_today"}, {0, 100, "upcoming"}}
	for i, w := range want {
		if got[i].Paid != w.paid || got[i].Remaining != w.remaining || got[i].Status != w.status {
			t.Errorf("installment %d: %+v, want %+v", i+1, got[i], w)
		}
	}

	got = Allocate(items, 50, d("2026-12-20"))
	if got[0].Status != "overdue" || got[1].Status != "overdue" || got[2].Status != "upcoming" {
		t.Errorf("overdue labels: %v %v %v", got[0].Status, got[1].Status, got[2].Status)
	}
	got = Allocate(items, 120, d("2026-10-01"))
	if got[1].Status != "partial" {
		t.Errorf("partial label: %v", got[1].Status)
	}
}

func TestTodayUsesTimezone(t *testing.T) {
	// Not asserting a value (depends on the clock); only that zones resolve and bad ones fall back.
	if Today("Asia/Dhaka").IsZero() || Today("Not/AZone").IsZero() {
		t.Fatal("zero date")
	}
	_ = time.Now()
}
