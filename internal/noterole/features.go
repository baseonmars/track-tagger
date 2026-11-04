// Package noterole proposes source-line roles. It does not determine parsed
// values, track mappings, recording readiness, or permission to write media.
package noterole

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const FeatureVersion = "noterole-unicode-v1"
const MaxLineRunes = 16384

// Lines defines the training/runtime representation, independently of catalog's
// structural parser: initial BOM removed, CRLF/CR normalized, terminal LF omitted.
func Lines(text string) ([]string, error) {
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	text = strings.TrimPrefix(text, "\ufeff")
	if text == "" {
		return []string{}, nil
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}
	for _, line := range lines {
		if utf8.RuneCountInString(line) > MaxLineRunes {
			return nil, fmt.Errorf("line exceeds rune bound")
		}
	}
	return lines, nil
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') })
}
func wordFeatures(out map[string]float64, prefix, s string) {
	w := words(s)
	for i, t := range w {
		out[prefix+"1:"+t]++
		if i > 0 {
			out[prefix+"2:"+w[i-1]+" "+t]++
		}
	}
}

// Features is the sole feature implementation used by offline training and Go
// inference. Prefixes keep line/character/neighbor vocabularies disjoint.
func Features(line, previous, next string) (map[string]float64, error) {
	for _, s := range []string{line, previous, next} {
		if !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxLineRunes {
			return nil, fmt.Errorf("invalid or oversized line")
		}
	}
	out := make(map[string]float64)
	wordFeatures(out, "w:", line)
	chars := []rune(strings.Join(strings.Fields(strings.ToLower(line)), " "))
	for n := 2; n <= 4; n++ {
		for i := 0; i+n <= len(chars); i++ {
			out[fmt.Sprintf("c:%d:%s", n, string(chars[i:i+n]))]++
		}
	}
	wordFeatures(out, "p:", previous)
	wordFeatures(out, "n:", next)
	if previous == "" {
		out["boundary:previous-empty"] = 1
	}
	if next == "" {
		out["boundary:next-empty"] = 1
	}
	return out, nil
}
