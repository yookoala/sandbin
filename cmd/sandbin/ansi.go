package main

import (
	"fmt"
	"html"
	"strconv"
	"strings"
)

// Browser defaults, used when a color has to be swapped by inverse video.
const (
	defaultFG = "#000000"
	defaultBG = "#ffffff"
)

// style is the SGR state a run of text carries.
type style struct {
	bold, faint, italic, underline, inverse bool
	fg, bg                                  string
}

type ansiRun struct {
	text string
	st   style
}

// ansiToHTML turns ANSI text into a <pre> with inline-styled spans so a browser
// can read a terminal capture.
// shortcut: carriage-return line overwrite is not modelled; \r is dropped, so a
// progress bar that redraws one line shows every frame glued together.
func ansiToHTML(s string) string {
	var b strings.Builder
	b.WriteString(`<pre class="ansi">`)
	for _, run := range parseANSI(s) {
		text := html.EscapeString(run.text)
		if css := run.st.css(); css != "" {
			fmt.Fprintf(&b, `<span style="%s">%s</span>`, css, text)
		} else {
			b.WriteString(text)
		}
	}
	b.WriteString("</pre>")
	return b.String()
}

// parseANSI splits text into runs, each with the style its escape sequences asked for.
func parseANSI(s string) []ansiRun {
	var runs []ansiRun
	st := style{}
	add := func(text string) {
		text = strings.ReplaceAll(text, "\r", "")
		if text == "" {
			return
		}
		if n := len(runs); n > 0 && runs[n-1].st == st {
			runs[n-1].text += text
			return
		}
		runs = append(runs, ansiRun{text: text, st: st})
	}

	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			end := i
			for end < len(s) && s[end] != 0x1b {
				end++
			}
			add(s[i:end])
			i = end
			continue
		}

		j := i + 1
		if j < len(s) && s[j] == '[' {
			j++
			start := j
			for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				applySGR(&st, s[start:j])
			}
			j++
		}
		// shortcut: escape sequences that are not SGR are dropped, not rendered.
		i = j
	}
	return runs
}

// applySGR interprets the parameters of a CSI ... m sequence.
func applySGR(st *style, params string) {
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			*st = style{} // an empty parameter means reset
			continue
		}
		switch {
		case n == 0:
			*st = style{}
		case n == 1:
			st.bold, st.faint = true, false
		case n == 2:
			st.bold, st.faint = false, true
		case n == 3:
			st.italic = true
		case n == 4:
			st.underline = true
		case n == 7:
			st.inverse = true
		case n == 22:
			st.bold, st.faint = false, false
		case n == 23:
			st.italic = false
		case n == 24:
			st.underline = false
		case n == 27:
			st.inverse = false
		case n >= 30 && n <= 37:
			st.fg = xtermColor(n - 30)
		case n == 39:
			st.fg = ""
		case n >= 40 && n <= 47:
			st.bg = xtermColor(n - 40)
		case n == 49:
			st.bg = ""
		case n >= 90 && n <= 97:
			st.fg = xtermColor(n - 90 + 8)
		case n == 38 || n == 48:
			// shortcut: only 38;5;n and 48;5;n are supported, truecolor is not.
			if i+2 < len(parts) && parts[i+1] == "5" {
				if c, err := strconv.Atoi(parts[i+2]); err == nil && c >= 0 && c < 256 {
					if n == 38 {
						st.fg = xtermColor(c)
					} else {
						st.bg = xtermColor(c)
					}
				}
				i += 2
			}
		}
	}
}

// css renders the style as an inline style attribute value.
func (st style) css() string {
	fg, bg := st.fg, st.bg
	if st.inverse {
		if fg == "" {
			fg = defaultFG
		}
		if bg == "" {
			bg = defaultBG
		}
		fg, bg = bg, fg
	}

	var props []string
	if fg != "" {
		props = append(props, "color:"+fg)
	}
	if bg != "" {
		props = append(props, "background-color:"+bg)
	}
	if st.bold {
		props = append(props, "font-weight:bold")
	}
	if st.faint {
		props = append(props, "opacity:0.5")
	}
	if st.italic {
		props = append(props, "font-style:italic")
	}
	if st.underline {
		props = append(props, "text-decoration:underline")
	}
	return strings.Join(props, ";")
}

// The 16 base xterm colors; 16-255 are computed from them.
var xtermBase = []string{
	"#000000", "#aa0000", "#00aa00", "#aa5500", "#0000aa", "#aa00aa", "#00aaaa", "#aaaaaa",
	"#555555", "#ff5555", "#55ff55", "#ffff55", "#5555ff", "#ff55ff", "#55ffff", "#ffffff",
}

var cubeLevels = []int{0, 95, 135, 175, 215, 255}

// xtermColor maps a 256-color palette index to a hex color.
func xtermColor(n int) string {
	switch {
	case n < 16:
		return xtermBase[n]
	case n < 232:
		n -= 16
		return fmt.Sprintf("#%02x%02x%02x", cubeLevels[n/36], cubeLevels[(n/6)%6], cubeLevels[n%6])
	case n < 256:
		grey := 8 + (n-232)*10
		return fmt.Sprintf("#%02x%02x%02x", grey, grey, grey)
	}
	return ""
}
