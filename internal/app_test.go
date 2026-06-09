package internal

import "testing"

func TestSlugify(t *testing.T) {
	got := Slugify("Hello World")
	want := "hello-world"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "Hello World", got, want)
	}
}

func TestSlugifyEmpty(t *testing.T) {
	got := Slugify("")
	if got != "" {
		t.Errorf("Slugify(%q) = %q, want %q", "", got, "")
	}
}

func TestSlugifySpecialChars(t *testing.T) {
	got := Slugify("Hello, World! #1")
	want := "hello-world-1"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "Hello, World! #1", got, want)
	}
}

func TestSlugifyLong(t *testing.T) {
	input := "this is a very long string that should definitely be truncated because it exceeds sixty characters in length"
	got := Slugify(input)
	if len(got) > 60 {
		t.Errorf("Slugify long string: len = %d, want <= 60", len(got))
	}
}

func TestSlugifyUnicode(t *testing.T) {
	got := Slugify("café résumé")
	// non-ascii stripped, only a-z0-9 kept; accented chars become separator
	// "café résumé" -> "caf-r-sum"
	if got == "" {
		t.Error("Slugify unicode: got empty string")
	}
	for _, c := range got {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			t.Errorf("Slugify unicode: got invalid char %q in %q", string(c), got)
		}
	}
}

func TestSlugifyAlreadySlugged(t *testing.T) {
	got := Slugify("already-slugged")
	want := "already-slugged"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "already-slugged", got, want)
	}
}

func TestSlugifyLeadingTrailingSpecial(t *testing.T) {
	got := Slugify("---hello---")
	want := "hello"
	if got != want {
		t.Errorf("Slugify(%q) = %q, want %q", "---hello---", got, want)
	}
}
