package notesmodel

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxLineBytes = 16 * 1024
	maxFocus     = 40
	maxContext   = 10
)

type offset struct{ start, end int }

type piece struct {
	id int
	offset
}

type window struct {
	note    string
	index   int
	line    []rune
	ids     []int
	offsets []offset
}

type tokenizer struct {
	vocabulary        map[string]int
	unknown, cls, sep int
}

func newTokenizer(vocabulary []string) tokenizer {
	values := make(map[string]int, len(vocabulary))
	for i, word := range vocabulary {
		values[word] = i
	}
	return tokenizer{values, values["[UNK]"], values["[CLS]"], values["[SEP]"]}
}

func (t tokenizer) window(name string, lines []string, index int) (window, bool) {
	if len(lines[index]) > maxLineBytes || !utf8.ValidString(lines[index]) {
		return window{}, false
	}
	line := []rune(lines[index])
	focus := t.encode(line)
	if len(focus) == 0 || len(focus) > maxFocus {
		return window{}, false
	}
	var before, after []piece
	if index > 0 && len(lines[index-1]) <= maxLineBytes && utf8.ValidString(lines[index-1]) {
		before = t.encode([]rune(lines[index-1]))
		before = before[max(0, len(before)-maxContext):]
	}
	if index+1 < len(lines) && len(lines[index+1]) <= maxLineBytes && utf8.ValidString(lines[index+1]) {
		after = t.encode([]rune(lines[index+1]))
		after = after[:min(maxContext, len(after))]
	}
	ids := make([]int, 0, len(focus)+len(before)+len(after)+4)
	offsets := make([]offset, 1, 1+len(focus))
	ids = append(ids, t.cls)
	for _, p := range focus {
		ids = append(ids, p.id)
		offsets = append(offsets, p.offset)
	}
	for _, context := range [][]piece{before, after} {
		ids = append(ids, t.sep)
		for _, p := range context {
			ids = append(ids, p.id)
		}
	}
	ids = append(ids, t.sep)
	return window{name, index, line, ids, offsets}, true
}

type character struct {
	value    rune
	position int
}

var special = [...]string{"[PAD]", "[UNK]", "[CLS]", "[SEP]", "[MASK]"}

func (t tokenizer) encode(line []rune) []piece {
	var result []piece
	var word []character
	flush := func() {
		result = t.wordPieces(result, word)
		word = word[:0]
	}
	for i := 0; i < len(line); i++ {
		r := line[i]
		if r == '[' {
			found := false
			for _, token := range special {
				end := i + len(token)
				if end <= len(line) && string(line[i:end]) == token {
					flush()
					result = append(result, piece{t.vocabulary[token], offset{i, end}})
					i = end - 1
					found = true
					break
				}
			}
			if found {
				continue
			}
		}
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' || unicode.Is(unicode.Zs, r) {
			flush()
			continue
		}
		if r == utf8.RuneError || unicode.Is(unicode.C, r) {
			continue
		}
		normalized := string(unicode.ToLower(r))
		if r >= utf8.RuneSelf {
			normalized = norm.NFD.String(normalized)
		}
		for _, c := range normalized {
			switch {
			case unicode.Is(unicode.Mn, c):
			case punctuation(c) || chinese(c):
				flush()
				result = t.wordPieces(result, []character{{c, i}})
			default:
				word = append(word, character{c, i})
			}
		}
	}
	flush()
	return result
}

func punctuation(r rune) bool {
	return (r >= '!' && r <= '/') || (r >= ':' && r <= '@') ||
		(r >= '[' && r <= '`') || (r >= '{' && r <= '~') || unicode.IsPunct(r)
}

func chinese(r rune) bool {
	return (0x4E00 <= r && r <= 0x9FFF) || (0x3400 <= r && r <= 0x4DBF) ||
		(0x20000 <= r && r <= 0x2A6DF) || (0x2A700 <= r && r <= 0x2B73F) ||
		(0x2B740 <= r && r <= 0x2B81F) || (0x2B820 <= r && r <= 0x2CEAF) ||
		(0xF900 <= r && r <= 0xFAFF) || (0x2F800 <= r && r <= 0x2FA1F)
}

func (t tokenizer) wordPieces(out []piece, word []character) []piece {
	if len(word) == 0 {
		return out
	}
	unknown := piece{t.unknown, offset{word[0].position, word[len(word)-1].position + 1}}
	if len(word) > 100 {
		return append(out, unknown)
	}
	var text strings.Builder
	var boundaries [101]int
	for i, char := range word {
		text.WriteRune(char.value)
		boundaries[i+1] = text.Len()
	}
	value := text.String()
	originalLen := len(out)
	for start := 0; start < len(word); {
		end := len(word)
		var id int
		for end > start {
			token := value[boundaries[start]:boundaries[end]]
			if start != 0 {
				token = "##" + token
			}
			var found bool
			id, found = t.vocabulary[token]
			if found {
				break
			}
			end--
		}
		if end == start {
			return append(out[:originalLen], unknown)
		}
		out = append(out, piece{id, offset{word[start].position, word[end-1].position + 1}})
		start = end
	}
	return out
}
