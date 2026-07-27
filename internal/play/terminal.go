package play

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/muesli/cancelreader"
	"golang.org/x/term"
)

func Interact(input io.Reader, output io.Writer, controller *Controller, done <-chan error, cancel func()) error {
	file, ok := input.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return <-done
	}
	state, err := term.MakeRaw(int(file.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(file.Fd()), state)

	reader, err := cancelreader.NewReader(file)
	if err != nil {
		return err
	}
	defer reader.Close()
	defer fmt.Fprintln(output)
	fmt.Fprint(output, "space: pause/resume  ↑/↓: volume  q: quit\r\n")

	keys := make(chan byte, 16)
	readErr := make(chan error, 1)
	go readKeys(reader, keys, readErr)
	decoder := keyDecoder{}
	for {
		select {
		case result := <-done:
			reader.Cancel()
			return result
		case err := <-readErr:
			if errors.Is(err, cancelreader.ErrCanceled) {
				return <-done
			}
			return err
		case key := <-keys:
			action, ok := decoder.Push(key)
			if !ok {
				continue
			}
			switch action {
			case actionToggle:
				paused, err := controller.Toggle()
				if err != nil {
					return err
				}
				if paused {
					fmt.Fprint(output, "\rpaused  ")
				} else {
					fmt.Fprint(output, "\rplaying ")
				}
			case actionUp:
				volume, err := controller.AdjustVolume(.05)
				if err != nil {
					return err
				}
				fmt.Fprintf(output, "\rvolume: %.0f%%  ", volume*100)
			case actionDown:
				volume, err := controller.AdjustVolume(-.05)
				if err != nil {
					return err
				}
				fmt.Fprintf(output, "\rvolume: %.0f%%  ", volume*100)
			case actionQuit:
				cancel()
				result := <-done
				if errors.Is(result, context.Canceled) {
					return nil
				}
				return result
			}
		}
	}
}

func readKeys(reader io.Reader, keys chan<- byte, result chan<- error) {
	var buffer [1]byte
	for {
		n, err := reader.Read(buffer[:])
		if n > 0 {
			keys <- buffer[0]
		}
		if err != nil {
			result <- err
			return
		}
	}
}

type action uint8

const (
	actionNone action = iota
	actionToggle
	actionUp
	actionDown
	actionQuit
)

type keyState uint8

const (
	keyNormal keyState = iota
	keyEscape
	keyCSI
	keyWindows
)

type keyDecoder struct{ state keyState }

func (d *keyDecoder) Push(key byte) (action, bool) {
	switch d.state {
	case keyEscape:
		d.state = keyNormal
		if key == '[' {
			d.state = keyCSI
			return actionNone, false
		}
		return decodeKey(key)
	case keyCSI:
		d.state = keyNormal
		switch key {
		case 'A':
			return actionUp, true
		case 'B':
			return actionDown, true
		default:
			return decodeKey(key)
		}
	case keyWindows:
		d.state = keyNormal
		switch key {
		case 'H':
			return actionUp, true
		case 'P':
			return actionDown, true
		default:
			return decodeKey(key)
		}
	}
	if key == 0x1b {
		d.state = keyEscape
		return actionNone, false
	}
	if key == 0 || key == 0xe0 {
		d.state = keyWindows
		return actionNone, false
	}
	return decodeKey(key)
}

func decodeKey(key byte) (action, bool) {
	switch key {
	case ' ':
		return actionToggle, true
	case 'q', 'Q':
		return actionQuit, true
	default:
		return actionNone, false
	}
}
