package dbx

import "testing"

// A []byte arrives for two unrelated reasons — MySQL returns text columns as
// bytes, and a bytea/BLOB/varbinary really is binary — so normaliseValue has to
// decide from the content. Getting it backwards either fills a MySQL database
// with hex or puts lossy mojibake and raw control bytes into the grid, the
// exports and the INSERT the row menu copies.
func TestNormaliseValueBytes(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want any
	}{
		{"plain text", []byte("hello"), "hello"},
		{"utf-8 text", []byte("héllo — ok"), "héllo — ok"},
		{"text with tabs and newlines", []byte("a\tb\nc\r\n"), "a\tb\nc\r\n"},
		{"empty", []byte{}, ""},
		{"png header", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, `\x89504e470d0a1a0a`},
		{"nul byte", []byte{'a', 0x00, 'b'}, `\x610062`},
		{"invalid utf-8", []byte{0xff, 0xfe}, `\xfffe`},
		{"del", []byte{0x7f}, `\x7f`},
	}
	for _, c := range cases {
		if got := normaliseValue(c.in); got != c.want {
			t.Errorf("%s: normaliseValue(%v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// A blob is cut off rather than rendered in full: a row is meant to be looked
// at, and an operator who wants the whole thing wants the export.
func TestNormaliseValueBoundsABlob(t *testing.T) {
	big := make([]byte, maxHexPreview*4)
	for i := range big {
		big[i] = 0xff
	}
	got, ok := normaliseValue(big).(string)
	if !ok {
		t.Fatalf("a large blob did not render as a string: %T", normaliseValue(big))
	}
	if len(got) > maxHexPreview*2+40 {
		t.Errorf("blob preview is %d characters, want it bounded near %d", len(got), maxHexPreview*2)
	}
	if !containsAll(got, `\x`, "bytes)") {
		t.Errorf("a truncated blob should say how big it was: %q", got[len(got)-40:])
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
