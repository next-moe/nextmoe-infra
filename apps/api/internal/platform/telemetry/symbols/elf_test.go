package symbols

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"os"
	"testing"
)

func noteGNU(desc []byte) []byte {
	return packNote("GNU", ntGNUBuildID, desc)
}

func packNote(name string, typ uint32, desc []byte) []byte {
	nameb := append([]byte(name), 0)
	namePad := (len(nameb) + 3) &^ 3
	descPad := (len(desc) + 3) &^ 3
	buf := make([]byte, 12+namePad+descPad)
	binary.LittleEndian.PutUint32(buf[0:], uint32(len(nameb)))
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(desc)))
	binary.LittleEndian.PutUint32(buf[8:], typ)
	copy(buf[12:], nameb)
	copy(buf[12+namePad:], desc)
	return buf
}

func makeELF(class elf.Class, machine elf.Machine, notes []byte) []byte {
	shstr := []byte("\x00.note.gnu.build-id\x00.shstrtab\x00")
	noteName, strName := 1, 21
	is64 := class == elf.ELFCLASS64
	var ehsize, shentsize int
	if is64 {
		ehsize, shentsize = 64, 64
	} else {
		ehsize, shentsize = 52, 40
	}
	shnum := 3
	if len(notes) == 0 {
		shnum = 2
		shstr = []byte("\x00.shstrtab\x00")
		strName = 1
	}
	noteOff := ehsize
	strOff := noteOff
	if len(notes) > 0 {
		strOff = noteOff + len(notes)
	}
	shoff := strOff + len(shstr)
	total := shoff + shentsize*shnum
	buf := make([]byte, total)
	buf[0], buf[1], buf[2], buf[3] = 0x7f, 'E', 'L', 'F'
	if is64 {
		buf[4] = 2
	} else {
		buf[4] = 1
	}
	buf[5] = 1
	buf[6] = 1
	put16 := func(off int, v uint16) { binary.LittleEndian.PutUint16(buf[off:], v) }
	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(buf[off:], v) }
	put64 := func(off int, v uint64) { binary.LittleEndian.PutUint64(buf[off:], v) }
	put16(16, 3)
	put16(18, uint16(machine))
	put32(20, 1)
	if is64 {
		put64(40, uint64(shoff))
		put16(52, uint16(ehsize))
		put16(58, uint16(shentsize))
		put16(60, uint16(shnum))
		put16(62, uint16(shnum-1))
	} else {
		put32(32, uint32(shoff))
		put16(40, uint16(ehsize))
		put16(46, uint16(shentsize))
		put16(48, uint16(shnum))
		put16(50, uint16(shnum-1))
	}
	if len(notes) > 0 {
		copy(buf[noteOff:], notes)
	}
	copy(buf[strOff:], shstr)
	writeShdr := func(i int, name uint32, typ uint32, off, size int, align uint64) {
		base := shoff + i*shentsize
		put32(base, name)
		put32(base+4, typ)
		if is64 {
			put64(base+24, uint64(off))
			put64(base+32, uint64(size))
			put64(base+48, align)
		} else {
			put32(base+16, uint32(off))
			put32(base+20, uint32(size))
			put32(base+32, uint32(align))
		}
	}
	idx := 1
	if len(notes) > 0 {
		writeShdr(1, uint32(noteName), uint32(elf.SHT_NOTE), noteOff, len(notes), 4)
		idx = 2
	}
	writeShdr(idx, uint32(strName), uint32(elf.SHT_STRTAB), strOff, len(shstr), 1)
	return buf
}

func TestBuildIDFromELF(t *testing.T) {
	id64 := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77}
	notes64 := append(packNote("FOO", 1, []byte("xxxx")), noteGNU(id64)...)
	b64 := makeELF(elf.ELFCLASS64, elf.EM_AARCH64, notes64)
	got, machine, err := BuildIDFromELF(bytes.NewReader(b64))
	if err != nil {
		t.Fatal(err)
	}
	if machine != elf.EM_AARCH64 {
		t.Fatalf("machine=%s", machine)
	}
	if got != "0123456789abcdef0011223344556677" {
		t.Fatalf("build-id=%s", got)
	}

	id32 := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xa0}
	notes32 := append(packNote("FOO", 1, []byte("xxxx")), noteGNU(id32)...)
	b32 := makeELF(elf.ELFCLASS32, elf.EM_ARM, notes32)
	got, machine, err = BuildIDFromELF(bytes.NewReader(b32))
	if err != nil {
		t.Fatal(err)
	}
	if machine != elf.EM_ARM {
		t.Fatalf("machine=%s", machine)
	}
	if got != "aabbccddeeff102030405060708090a0" {
		t.Fatalf("build-id=%s", got)
	}
}

func TestBuildIDMissing(t *testing.T) {
	b := makeELF(elf.ELFCLASS64, elf.EM_AARCH64, nil)
	_, _, err := BuildIDFromELF(bytes.NewReader(b))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildIDNotELF(t *testing.T) {
	_, _, err := BuildIDFromELF(bytes.NewReader([]byte("not elf")))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildIDRealSample(t *testing.T) {
	path := os.Getenv("TELEMETRY_SAMPLE_SYMBOLS")
	if path == "" {
		t.Skip("TELEMETRY_SAMPLE_SYMBOLS unset")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	id, machine, err := BuildIDFromELF(f)
	if err != nil {
		t.Fatal(err)
	}
	if id != "00c92cc1f2db62bbc0baf45738c85488" {
		t.Fatalf("build-id=%s", id)
	}
	if machine != elf.EM_AARCH64 {
		t.Fatalf("machine=%s", machine)
	}
}
