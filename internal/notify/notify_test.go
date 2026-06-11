package notify

import "testing"

func TestQuoting(t *testing.T) {
	if got := osaStr(`a"b\c`); got != `"a\"b\\c"` {
		t.Fatalf("osaStr: %s", got)
	}
	if got := psStr("it's"); got != "'it''s'" {
		t.Fatalf("psStr: %s", got)
	}
}

func TestWinToastEscapes(t *testing.T) {
	s := winToast("mtop", "GPU 'X' at 95%")
	if want := "'GPU ''X'' at 95%'"; !contains(s, want) {
		t.Fatalf("message not escaped into script: %s", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
