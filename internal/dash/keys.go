package dash

import "github.com/charmbracelet/bubbles/key"

// KeyMap groups every key binding used by the dash.
type KeyMap struct {
	Quit       key.Binding
	Tab        key.Binding
	ShiftTab   key.Binding
	Up         key.Binding
	Down       key.Binding
	Prompt     key.Binding
	Help       key.Binding
	Buy        key.Binding
	Sell       key.Binding
	Close      key.Binding
	Cancel     key.Binding
	Advisor    key.Binding
	Refresh    key.Binding
	Apply      key.Binding
	Ignore     key.Binding
	Modify     key.Binding
	Watch1     key.Binding
	Watch2     key.Binding
	Watch3     key.Binding
	Watch4     key.Binding
	Watch5     key.Binding
	Watch6     key.Binding
	Watch7     key.Binding
	Watch8     key.Binding
	Watch9     key.Binding
	ConfirmY   key.Binding
	ConfirmN   key.Binding
	Escape     key.Binding
	Enter      key.Binding
}

// DefaultKeyMap returns the standard bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "focus")),
		ShiftTab: key.NewBinding(key.WithKeys("shift+tab")),
		Up:       key.NewBinding(key.WithKeys("up", "k")),
		Down:     key.NewBinding(key.WithKeys("down", "j")),
		Prompt:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "prompt")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Buy:      key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "buy")),
		Sell:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sell")),
		Close:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "close")),
		Cancel:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "cancel")),
		Advisor:  key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "advisor")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Apply:    key.NewBinding(key.WithKeys("a")),
		Ignore:   key.NewBinding(key.WithKeys("i")),
		Modify:   key.NewBinding(key.WithKeys("m")),
		Watch1:   key.NewBinding(key.WithKeys("1")),
		Watch2:   key.NewBinding(key.WithKeys("2")),
		Watch3:   key.NewBinding(key.WithKeys("3")),
		Watch4:   key.NewBinding(key.WithKeys("4")),
		Watch5:   key.NewBinding(key.WithKeys("5")),
		Watch6:   key.NewBinding(key.WithKeys("6")),
		Watch7:   key.NewBinding(key.WithKeys("7")),
		Watch8:   key.NewBinding(key.WithKeys("8")),
		Watch9:   key.NewBinding(key.WithKeys("9")),
		ConfirmY: key.NewBinding(key.WithKeys("y", "Y")),
		ConfirmN: key.NewBinding(key.WithKeys("n", "N")),
		Escape:   key.NewBinding(key.WithKeys("esc")),
		Enter:    key.NewBinding(key.WithKeys("enter")),
	}
}
