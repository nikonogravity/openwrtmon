//go:build !windows

package flyout

type stubManager struct{}

func (s *stubManager) ShowNear(x, y int32) {}
func (s *stubManager) Hide()               {}
func (s *stubManager) Toggle(x, y int32)   {}
func (s *stubManager) Update(state State)  {}
func (s *stubManager) Destroy()            {}

// New creates a new flyout manager. On non-windows it returns a stub.
func New(cb Callbacks) Manager {
	return &stubManager{}
}

