package main

import (
	"testing"
)

func TestANSIToHTMLPlain(t *testing.T) {
	got := ansiToHTML("plain text")
	want := `<pre class="ansi">plain text</pre>`
	if got != want {
		t.Errorf("ansiToHTML(plain) = %q, want %q", got, want)
	}
}

func TestANSIToHTMLBoldColorReset(t *testing.T) {
	got := ansiToHTML("\x1b[1;31mred bold\x1b[0m plain")
	want := `<pre class="ansi"><span style="color:#aa0000;font-weight:bold">red bold</span> plain</pre>`
	if got != want {
		t.Errorf("bold + color + reset = %q, want %q", got, want)
	}

	got = ansiToHTML("\x1b[32mgreen\x1b[39m default")
	want = `<pre class="ansi"><span style="color:#00aa00">green</span> default</pre>`
	if got != want {
		t.Errorf("color then 39 = %q, want %q", got, want)
	}

	got = ansiToHTML("\x1b[1mbold\x1b[22mnormal")
	want = `<pre class="ansi"><span style="font-weight:bold">bold</span>normal</pre>`
	if got != want {
		t.Errorf("bold then 22 = %q, want %q", got, want)
	}
}

func TestANSIToHTMLAttributes(t *testing.T) {
	got := ansiToHTML("\x1b[2;3;4mfaint italic under\x1b[22;23;24m rest")
	want := `<pre class="ansi"><span style="opacity:0.5;font-style:italic;text-decoration:underline">faint italic under</span> rest</pre>`
	if got != want {
		t.Errorf("attributes = %q, want %q", got, want)
	}

	got = ansiToHTML("\x1b[7minverted\x1b[27m plain")
	want = `<pre class="ansi"><span style="color:#ffffff;background-color:#000000">inverted</span> plain</pre>`
	if got != want {
		t.Errorf("inverse = %q, want %q", got, want)
	}

	got = ansiToHTML("\x1b[41mred background\x1b[49m plain")
	want = `<pre class="ansi"><span style="background-color:#aa0000">red background</span> plain</pre>`
	if got != want {
		t.Errorf("background = %q, want %q", got, want)
	}
}

func TestANSIToHTMLColors(t *testing.T) {
	cases := []struct {
		seq  string
		want string
	}{
		{"\x1b[30mblack\x1b[0m", `<span style="color:#000000">black</span>`},
		{"\x1b[37mwhite\x1b[0m", `<span style="color:#aaaaaa">white</span>`},
		{"\x1b[93mbright yellow\x1b[0m", `<span style="color:#ffff55">bright yellow</span>`},
		{"\x1b[38;5;196mfg cube\x1b[0m", `<span style="color:#ff0000">fg cube</span>`},
		{"\x1b[38;5;21mfg cube\x1b[0m", `<span style="color:#0000ff">fg cube</span>`},
		{"\x1b[48;5;46mbg cube\x1b[0m", `<span style="background-color:#00ff00">bg cube</span>`},
		{"\x1b[38;5;232mdark grey\x1b[0m", `<span style="color:#080808">dark grey</span>`},
		{"\x1b[38;5;255mlight grey\x1b[0m", `<span style="color:#eeeeee">light grey</span>`},
		{"\x1b[38;5;196;48;5;21mboth\x1b[0m", `<span style="color:#ff0000;background-color:#0000ff">both</span>`},
	}
	for _, c := range cases {
		got := ansiToHTML(c.seq)
		want := `<pre class="ansi">` + c.want + `</pre>`
		if got != want {
			t.Errorf("ansiToHTML(%q) = %q, want %q", c.seq, got, want)
		}
	}
}

func TestANSIToHTMLEscapesHTML(t *testing.T) {
	got := ansiToHTML("<script>alert(1)</script>\x1b[1m<b>bold</b>\x1b[0m")
	want := `<pre class="ansi">&lt;script&gt;alert(1)&lt;/script&gt;<span style="font-weight:bold">&lt;b&gt;bold&lt;/b&gt;</span></pre>`
	if got != want {
		t.Errorf("escaping = %q, want %q", got, want)
	}
}

func TestANSIToHTMLOtherSequences(t *testing.T) {
	got := ansiToHTML("\x1b[2Jcleared")
	want := `<pre class="ansi">cleared</pre>`
	if got != want {
		t.Errorf("non-SGR sequence = %q, want %q", got, want)
	}

	got = ansiToHTML("\x1b[38;2;255;100;0mtruecolor\x1b[0m")
	want = `<pre class="ansi">truecolor</pre>`
	if got != want {
		t.Errorf("truecolor = %q, want it ignored", got)
	}

	got = ansiToHTML("\x1b[mreset only")
	want = `<pre class="ansi">reset only</pre>`
	if got != want {
		t.Errorf("bare reset = %q, want %q", got, want)
	}

	got = ansiToHTML("one\rframe\r\ntwo")
	want = "<pre class=\"ansi\">oneframe\ntwo</pre>"
	if got != want {
		t.Errorf("carriage returns = %q, want %q", got, want)
	}

	got = ansiToHTML("a\nb\x1b[1m\nbold\n\x1b[0mplain")
	want = "<pre class=\"ansi\">a\nb<span style=\"font-weight:bold\">\nbold\n</span>plain</pre>"
	if got != want {
		t.Errorf("newlines = %q, want %q", got, want)
	}
}
