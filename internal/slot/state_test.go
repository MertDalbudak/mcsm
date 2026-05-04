package slot

import "testing"

func TestStateTerminal(t *testing.T) {
	cases := map[State]bool{
		StateIdle:     true,
		StateMounting: false,
		StateStarting: false,
		StateRunning:  false,
		StateStopping: false,
		StateCrashed:  true,
		StateError:    true,
	}
	for s, want := range cases {
		if got := s.Terminal(); got != want {
			t.Errorf("%s.Terminal()=%v want %v", s, got, want)
		}
	}
}

func TestStateCanStartFrom(t *testing.T) {
	cases := map[State]bool{
		StateIdle:     true,
		StateCrashed:  true,
		StateError:    true,
		StateMounting: false,
		StateStarting: false,
		StateRunning:  false,
		StateStopping: false,
	}
	for s, want := range cases {
		if got := s.CanStartFrom(); got != want {
			t.Errorf("%s.CanStartFrom()=%v want %v", s, got, want)
		}
	}
}

func TestStateCanStopFrom(t *testing.T) {
	cases := map[State]bool{
		StateRunning:  true,
		StateStarting: true,
		StateIdle:     false,
		StateMounting: false,
		StateStopping: false,
		StateCrashed:  false,
		StateError:    false,
	}
	for s, want := range cases {
		if got := s.CanStopFrom(); got != want {
			t.Errorf("%s.CanStopFrom()=%v want %v", s, got, want)
		}
	}
}
