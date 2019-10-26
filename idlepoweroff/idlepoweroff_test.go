package main

import (
	"io"
	"os"
	"testing"
)

type emptyFile struct{}

func (*emptyFile) Read([]byte) (n int, _ error) { return 0, io.EOF }
func (*emptyFile) Close() error                 { return nil }

func TestBusy(t *testing.T) {
	t.Run("EmptyFile", func(t *testing.T) {
		s := state{
			open: func() (io.ReadCloser, error) {
				return &emptyFile{}, nil
			},
		}
		if got, want := s.busy(), false; got != want {
			t.Errorf("busy() = %v, want %v", got, want)
		}
	})

	t.Run("NonEmptyUtmp", func(t *testing.T) {
		s := state{
			open: func() (io.ReadCloser, error) {
				return os.Open("testdata/utmp")
			},
		}
		if got, want := s.busy(), true; got != want {
			t.Errorf("busy() = %v, want %v", got, want)
		}
	})
}
