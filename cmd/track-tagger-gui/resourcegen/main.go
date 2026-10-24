// Command resourcegen emits deterministic Windows COFF resources without external tools.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

const manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0"><assemblyIdentity version="1.0.0.0" processorArchitecture="*" name="TrackTagger.Launcher" type="win32"/><description>Track Tagger</description><trustInfo xmlns="urn:schemas-microsoft-com:asm.v3"><security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security></trustInfo><application xmlns="urn:schemas-microsoft-com:asm.v3"><windowsSettings><dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware></windowsSettings></application></assembly>`

func put(b []byte, off int, v uint32) { binary.LittleEndian.PutUint32(b[off:], v) }
func icon() []byte {
	b := make([]byte, 40+32*32*4+32*4)
	put(b, 0, 40)
	put(b, 4, 32)
	put(b, 8, 64)
	binary.LittleEndian.PutUint16(b[12:], 1)
	binary.LittleEndian.PutUint16(b[14:], 32)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			i := 40 + (y*32+x)*4
			b[i] = 0x67
			b[i+1] = 0x45
			b[i+2] = 0x21
			b[i+3] = 255
			if (x >= 16 && x <= 19 && y >= 8 && y <= 25) || (y >= 23 && y <= 26 && x >= 17 && x <= 26) || ((x-12)*(x-12)+(y-8)*(y-8) < 25) {
				b[i] = 255
				b[i+1] = 255
				b[i+2] = 255
			}
		}
	}
	return b
}
func resource() ([]byte, []uint32) {
	im := icon()
	group := make([]byte, 20)
	binary.LittleEndian.PutUint16(group[2:], 1)
	binary.LittleEndian.PutUint16(group[4:], 1)
	group[6] = 32
	group[7] = 32
	binary.LittleEndian.PutUint16(group[10:], 1)
	binary.LittleEndian.PutUint16(group[12:], 32)
	put(group, 14, uint32(len(im)))
	binary.LittleEndian.PutUint16(group[18:], 1)
	blobs := [][]byte{im, group, []byte(manifest)}
	b := make([]byte, 256)
	binary.LittleEndian.PutUint16(b[14:], 3)
	types := []uint32{3, 14, 24}
	var rel []uint32
	for i, typ := range types {
		typeDir := 40 + i*24
		langDir := 112 + i*24
		entry := 184 + i*16
		put(b, 16+i*8, typ)
		put(b, 20+i*8, uint32(typeDir)|0x80000000)
		binary.LittleEndian.PutUint16(b[typeDir+14:], 1)
		put(b, typeDir+16, 1)
		put(b, typeDir+20, uint32(langDir)|0x80000000)
		binary.LittleEndian.PutUint16(b[langDir+14:], 1)
		put(b, langDir+16, 1033)
		put(b, langDir+20, uint32(entry))
		put(b, entry, uint32(len(b)))
		put(b, entry+4, uint32(len(blobs[i])))
		rel = append(rel, uint32(entry))
		b = append(b, blobs[i]...)
		for len(b)%4 != 0 {
			b = append(b, 0)
		}
	}
	return b, rel
}
func object(machine, kind uint16) []byte {
	r, rel := resource()
	b := make([]byte, 60)
	binary.LittleEndian.PutUint16(b, machine)
	binary.LittleEndian.PutUint16(b[2:], 1)
	put(b, 8, uint32(60+len(r)+10*len(rel)))
	put(b, 12, 1)
	copy(b[20:], ".rsrc")
	put(b, 36, uint32(len(r)))
	put(b, 40, 60)
	put(b, 44, uint32(60+len(r)))
	binary.LittleEndian.PutUint16(b[52:], uint16(len(rel)))
	put(b, 56, 0x40000040)
	out := bytes.NewBuffer(b)
	out.Write(r)
	for _, off := range rel {
		binary.Write(out, binary.LittleEndian, off)
		binary.Write(out, binary.LittleEndian, uint32(0))
		binary.Write(out, binary.LittleEndian, kind)
	}
	sym := make([]byte, 18)
	copy(sym, ".rsrc")
	binary.LittleEndian.PutUint16(sym[12:], 1)
	sym[16] = 3
	out.Write(sym)
	binary.Write(out, binary.LittleEndian, uint32(4))
	return out.Bytes()
}
func main() {
	base := filepath.Join("cmd", "track-tagger-gui")
	if _, err := os.Stat(filepath.Join(base, "main_windows.go")); err != nil {
		panic("run resourcegen from repository root")
	}
	for _, a := range []struct {
		name           string
		machine, reloc uint16
	}{{"amd64", 0x8664, 3}, {"arm64", 0xaa64, 2}} {
		p := filepath.Join(base, "resources_windows_"+a.name+".syso")
		if err := os.WriteFile(p, object(a.machine, a.reloc), 0644); err != nil {
			panic(err)
		}
		fmt.Println(p)
	}
}
