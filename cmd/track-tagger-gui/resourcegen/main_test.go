package main

import (
	"bytes"
	"debug/pe"
	"testing"
)

func TestResources(t *testing.T) {
	for _, arch := range []struct{ machine, rel uint16 }{{0x8664, 3}, {0xaa64, 2}} {
		b := object(arch.machine, arch.rel)
		f, e := pe.NewFile(bytes.NewReader(b))
		if e != nil {
			t.Fatal(e)
		}
		if f.Machine != arch.machine || len(f.Sections) != 1 || f.Sections[0].Name != ".rsrc" {
			t.Fatal("invalid COFF headers")
		}
		s := f.Sections[0]
		if len(s.Relocs) != 3 {
			t.Fatalf("relocations: %d", len(s.Relocs))
		}
		for _, r := range s.Relocs {
			if r.Type != arch.rel {
				t.Fatal("incorrect relocation")
			}
		}
		if !bytes.Equal(b, object(arch.machine, arch.rel)) {
			t.Fatal("resources not reproducible")
		}
		f.Close()
	}
}
