package overlay

// Prompt is a small state struct for the NL prompt overlay.
type Prompt struct {
	Text   string
	Cursor int
}

// Insert appends runes at the cursor.
func (p *Prompt) Insert(s string) {
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor > len(p.Text) {
		p.Cursor = len(p.Text)
	}
	p.Text = p.Text[:p.Cursor] + s + p.Text[p.Cursor:]
	p.Cursor += len(s)
}

// Backspace deletes the rune before the cursor.
func (p *Prompt) Backspace() {
	if p.Cursor <= 0 {
		return
	}
	p.Text = p.Text[:p.Cursor-1] + p.Text[p.Cursor:]
	p.Cursor--
}

// Reset clears the prompt.
func (p *Prompt) Reset() {
	p.Text = ""
	p.Cursor = 0
}

// Value returns the current text.
func (p *Prompt) Value() string { return p.Text }
