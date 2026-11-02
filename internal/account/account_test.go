package account

import "testing"

func TestSettingsValidate(t *testing.T) {
	valid := Settings{Name: "My ledger", Currency: "BDT", Timezone: "Asia/Dhaka", SendHour: 9, Locale: "bn"}
	if errs := valid.Validate(); len(errs) > 0 {
		t.Fatalf("valid settings rejected: %v", errs)
	}

	cases := map[string]struct {
		mut   func(*Settings)
		field string
	}{
		"blank name":       {func(s *Settings) { s.Name = " " }, "name"},
		"lowercase money":  {func(s *Settings) { s.Currency = "bdt" }, "currency"},
		"unknown timezone": {func(s *Settings) { s.Timezone = "Mars/Base" }, "timezone"},
		"Local timezone":   {func(s *Settings) { s.Timezone = "Local" }, "timezone"},
		"hour 24":          {func(s *Settings) { s.SendHour = 24 }, "send_hour"},
		"locale fr":        {func(s *Settings) { s.Locale = "fr" }, "locale"},
	}
	for name, c := range cases {
		s := valid
		c.mut(&s)
		if errs := s.Validate(); errs[c.field] == "" {
			t.Errorf("%s: expected error on %s, got %v", name, c.field, errs)
		}
	}
}

func TestSettingsNormalize(t *testing.T) {
	s := Settings{Name: "  Shop ", Currency: " usd ", Timezone: " UTC "}
	s.Normalize()
	if s.Name != "Shop" || s.Currency != "USD" || s.Timezone != "UTC" {
		t.Fatalf("got %+v", s)
	}
}
