//go:build !darwin

package browserserve

// WatchCoworkLog is only implemented on macOS (Cowork is macOS-only).
func (s *Server) WatchCoworkLog() {}
