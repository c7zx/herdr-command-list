package main

import "unicode/utf8"

var terminalKeys = keyReader{next: readByte}

func readKey() (string, rune, error) { return terminalKeys.read() }

type byteReader func() (byte, bool, error)

type keyReader struct {
	next       byteReader
	pasting    bool
	pasteMatch int
	sequence   []byte
	inSequence bool
	overflow   bool
}

func readKeyFrom(next byteReader) (string, rune, error) {
	reader := keyReader{next: next}
	return reader.read()
}

func (reader *keyReader) read() (string, rune, error) {
	if reader.pasting {
		return reader.discardPaste()
	}
	if reader.inSequence {
		return reader.readSequence()
	}
	first, ok, err := reader.next()
	if err != nil || !ok {
		return "", 0, err
	}
	if first != 0x1b {
		return readRuneKey(first, reader.next)
	}
	second, ok, err := reader.next()
	if err != nil || !ok {
		return "escape", 0, err
	}
	if second != '[' && second != 'O' {
		return "", 0, nil // Ignore terminal Alt/meta combinations.
	}
	reader.sequence = reader.sequence[:0]
	reader.inSequence, reader.overflow = true, false
	return reader.readSequence()
}

func (reader *keyReader) readSequence() (string, rune, error) {
	// Consume complete CSI/SS3 sequences so unsupported terminal keys cannot
	// leak trailing bytes into the filter or act as Enter.
	for {
		b, ok, err := reader.next()
		if err != nil || !ok {
			return "", 0, err
		}
		if len(reader.sequence) < 64 {
			reader.sequence = append(reader.sequence, b)
		} else {
			reader.overflow = true
		}
		if b >= 0x40 && b <= 0x7e {
			reader.inSequence = false
			if reader.overflow {
				return "", 0, nil
			}
			if string(reader.sequence) == "200~" {
				reader.pasting = true
				return reader.discardPaste()
			}
			return decodeCSI(string(reader.sequence)), 0, nil
		}
	}
}

// Clipboard newlines must never act as Enter. Keep this state across terminal
// read timeouts, and discard without accumulating an unbounded pasted block.
func (reader *keyReader) discardPaste() (string, rune, error) {
	const end = "\x1b[201~"
	for {
		b, ok, err := reader.next()
		if err != nil || !ok {
			return "", 0, err
		}
		if b == end[reader.pasteMatch] {
			reader.pasteMatch++
		} else if b == end[0] {
			reader.pasteMatch = 1
		} else {
			reader.pasteMatch = 0
		}
		if reader.pasteMatch == len(end) {
			reader.pasting, reader.pasteMatch = false, 0
			return "", 0, nil
		}
	}
}

func readRuneKey(first byte, next byteReader) (string, rune, error) {
	if first < utf8.RuneSelf {
		key, r := runeKey(rune(first))
		return key, r, nil
	}
	buf := []byte{first}
	for !utf8.FullRune(buf) && len(buf) < utf8.UTFMax {
		b, ok, err := next()
		if err != nil || !ok {
			return "", 0, err
		}
		buf = append(buf, b)
	}
	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size == 1 {
		return "", 0, nil
	}
	key, r := runeKey(r)
	return key, r, nil
}

func decodeCSI(sequence string) string {
	switch sequence {
	case "A", "1A", "1;1A":
		return "up"
	case "B", "1B", "1;1B":
		return "down"
	case "C", "1C", "1;1C":
		return "right"
	case "D", "1D", "1;1D":
		return "left"
	default:
		return ""
	}
}

func runeKey(r rune) (string, rune) {
	switch r {
	case 0:
		return "ctrl+space", 0
	case 9:
		return "", 0 // Tab is intentionally unused.
	case 10, 13:
		return "enter", 0
	case 8, 127:
		return "backspace", 0
	case 27:
		return "escape", 0
	case '?':
		return "help", 0
	}
	if r >= 1 && r <= 26 {
		return "ctrl+" + string('a'+r-1), 0
	}
	if !unsafeRune(r) {
		return "rune", r
	}
	return "", 0
}
