package symbols

import (
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
)

const ntGNUBuildID = 3

func BuildIDFromELF(r io.ReaderAt) (id string, machine elf.Machine, err error) {
	f, err := elf.NewFile(r)
	if err != nil {
		return "", 0, fmt.Errorf("not elf: %w", err)
	}
	defer f.Close()
	machine = f.Machine
	for _, sec := range f.Sections {
		if sec.Type != elf.SHT_NOTE {
			continue
		}
		data, err := sec.Data()
		if err != nil {
			return "", machine, err
		}
		id, ok := gnuBuildIDFromNotes(data)
		if ok {
			return id, machine, nil
		}
	}
	return "", machine, fmt.Errorf("missing gnu build-id")
}

func gnuBuildIDFromNotes(data []byte) (string, bool) {
	off := 0
	for off+12 <= len(data) {
		namesz := binary.LittleEndian.Uint32(data[off:])
		descsz := binary.LittleEndian.Uint32(data[off+4:])
		typ := binary.LittleEndian.Uint32(data[off+8:])
		off += 12
		namePad := (int(namesz) + 3) &^ 3
		descPad := (int(descsz) + 3) &^ 3
		if namesz > uint32(len(data)) || descsz > uint32(len(data)) || off+namePad+descPad > len(data) {
			return "", false
		}
		name := data[off : off+int(namesz)]
		off += namePad
		desc := data[off : off+int(descsz)]
		off += descPad
		if typ == ntGNUBuildID && gnuNoteName(name) {
			return hex.EncodeToString(desc), true
		}
	}
	return "", false
}

func gnuNoteName(name []byte) bool {
	if len(name) > 0 && name[len(name)-1] == 0 {
		name = name[:len(name)-1]
	}
	return string(name) == "GNU"
}
