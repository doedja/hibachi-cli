package overlay

// Confirm is a tiny confirm modal state primitive.
type Confirm struct {
	Title   string
	Message string
	OnYes   func()
	OnNo    func()
}
