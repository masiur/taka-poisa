package auth

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword("correct horse", h); err != nil || !ok {
		t.Fatalf("right password: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword("wrong horse", h); err != nil || ok {
		t.Fatalf("wrong password: ok=%v err=%v", ok, err)
	}
	h2, _ := HashPassword("correct horse")
	if h == h2 {
		t.Fatal("hashes of the same password must differ (salt)")
	}
}

func TestVerifyPasswordMalformed(t *testing.T) {
	for _, h := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$c2FsdA$a2V5", "$argon2id$v=19$m=x$c2FsdA$a2V5", "$argon2id$v=19$m=1,t=1,p=1$!!$a2V5"} {
		if _, err := VerifyPassword("x", h); err == nil {
			t.Errorf("expected error for %q", h)
		}
	}
}
