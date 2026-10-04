package nlp

import (
	"reflect"
	"testing"
)

func TestNormalizeInput(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  hello  ", "hello"},
		{"line one\r\nline two\r\n", "line one\nline two"},
		{"cr only\rlines", "cr only\nlines"},
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeInput(c.in); got != c.want {
			t.Errorf("NormalizeInput(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestClassifyKind(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"suspended", KindWord},
		{"Running", KindWord},
		{"don't", KindWord},
		{"state-of-the-art", KindWord},
		{"Hello, world", KindText},
		{"two words", KindText},
		{"hello123", KindText},
		{"this is a much longer sentence that goes beyond forty characters total", KindText},
	}
	for _, c := range cases {
		if got := ClassifyKind(c.in); got != c.want {
			t.Errorf("ClassifyKind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	if !IsSupportedLanguage("Hello, world! This is English with a bit of 你好 mixed in.") {
		t.Errorf("English-dominant mixed text should be supported")
	}
	if IsSupportedLanguage("这是一段纯中文文本") {
		t.Errorf("Chinese-dominant text must be unsupported")
	}
	if IsSupportedLanguage("hello 测试测试测试测试") {
		t.Errorf("non-English dominant text must be unsupported")
	}
}

func TestTokenize(t *testing.T) {
	got := Tokenize("Hello, world! It's a fine day; visit https://example.com/x?q=1 and see foo_bar = 42 or `code_span` and stop.")
	want := []string{"Hello", "world", "It's", "a", "fine", "day", "visit", "and", "see", "or", "and", "stop"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize = %v, want %v", got, want)
	}

	got = Tokenize("Numbers 123 and 3.14 and COVID-19 stay out as numbers.")
	for _, tok := range got {
		if tok == "123" || tok == "3" || tok == "14" || tok == "19" {
			t.Errorf("number token leaked: %v", got)
		}
	}
}

func TestLemmatize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"running", "run"},
		{"studies", "study"},
		{"stopped", "stop"},
		{"suspended", "suspend"},
		{"cats", "cat"},
		{"boxes", "box"},
		{"watches", "watch"},
		{"class", "class"},
		{"king", "king"},       // conservative fallback: stem too short
		{"session", "session"}, // "ss" not stripped
	}
	for _, c := range cases {
		if got := Lemmatize(c.in); got != c.want {
			t.Errorf("Lemmatize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
