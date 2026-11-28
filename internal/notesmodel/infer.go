package notesmodel

import (
	"context"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	label       = regexp.MustCompile(`(?i)\b(artist|performer|band|concert date|date|venue|city)\s*:\s*`)
	section     = regexp.MustCompile(`(?i)^(?:set\s+(?:\d+|[ivx]+|one|two)\b|(?:disc|disk|cd)\s+\d+\b|\d{1,3}\s*[.)_-]\s*\S|d\d+t\d+|(?:source|lineage|track\s*list|set\s*list|notes|credits|personnel|musicians|recording|transfer|taper|checksums?)\s*:)`)
	unsafeText  = regexp.MustCompile(`(?i)\b(?:last time|previous(?:ly)?|formerly|guest|featur\w*|taper|lineage|transfer\w*|upload\w*|seeded|source|mastered|recorded by|taped by|mixed by|another show|probably|possibly|perhaps|unsure|uncertain|unknown|or maybe)\b|\bw/|[?]|\.(?:flac|shn|wav|mp3|txt|nfo|md5|ffp)\b`)
	numericDate = regexp.MustCompile(`^(\d{1,4})([-/.])(\d{1,2})([-/.])(\d{1,4})$`)
	ordinalDate = regexp.MustCompile(`(?i)([0-9])(?:st|nd|rd|th)\b`)
	weekday     = regexp.MustCompile(`(?i)^(?:mon(?:day)?|tue(?:sday)?|wed(?:nesday)?|thu(?:rsday)?|fri(?:day)?|sat(?:urday)?|sun(?:day)?)[,\.]?\s+`)
)

// dateLike recognizes whole date spans without choosing between the US and
// day-first readings. The production resolver makes that evidence decision.
func dateLike(value string) bool {
	value = weekday.ReplaceAllString(strings.TrimSpace(value), "")
	if match := numericDate.FindStringSubmatch(value); match != nil {
		if match[2] != match[4] {
			return false
		}
		first, _ := strconv.Atoi(match[1])
		middle, _ := strconv.Atoi(match[3])
		last, _ := strconv.Atoi(match[5])
		if len(match[1]) == 4 {
			return calendarDay(first, middle, last)
		}
		return len(match[5]) == 4 && (calendarDay(last, first, middle) || calendarDay(last, middle, first))
	}
	value = ordinalDate.ReplaceAllString(value, "$1")
	value = strings.Join(strings.Fields(strings.ReplaceAll(value, ",", " ")), " ")
	for _, layout := range []string{"January 2 2006", "Jan 2 2006", "2 January 2006", "2 Jan 2006"} {
		if date, err := time.Parse(layout, value); err == nil {
			return date.Year() >= 1900 && date.Year() <= 2099
		}
	}
	return false
}

func calendarDay(year, month, day int) bool {
	if year < 1900 || year > 2099 || month < 1 || month > 12 || day < 1 || day > 31 {
		return false
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC).Day() == day
}

func (m *Model) variantSpans(ctx context.Context, windows []window, variant int) ([]Span, error) {
	var proposals []Span
	e := &m.encoders[variant]
	for _, sample := range windows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		scores := e.logits(sample.ids, len(sample.offsets))
		for i, role := range fields {
			start, end, best := bestSpan(scores, i, len(sample.offsets))
			// Match the experiment's six-decimal score comparison.
			if math.RoundToEven(float64(best)*1e6)/1e6 < float64(e.thresholds[i]) {
				continue
			}
			start, end = sample.offsets[start].start, sample.offsets[end].end
			if validQuote(sample.line, role, start, end) {
				proposals = append(proposals, Span{role, sample.note, sample.index, start, end})
			}
		}
	}
	return proposals, ctx.Err()
}

func bestSpan(scores []float32, role, count int) (int, int, float32) {
	const logits = 8
	start, end := 0, 0
	best := float32(math.Inf(-1))
	role *= 2
	for i := 1; i < count; i++ {
		for j := i; j < count && j-i < 24; j++ {
			score := scores[i*logits+role] + scores[j*logits+role+1] - scores[role] - scores[role+1]
			if score > best {
				start, end, best = i, j, score
			}
		}
	}
	return start, end, best
}

func validQuote(line []rune, field string, start, end int) bool {
	if start >= end || unicode.IsSpace(line[start]) || unicode.IsSpace(line[end-1]) {
		return false
	}
	quote := string(line[start:end])
	if strings.ContainsRune(":|,;", line[start]) || strings.ContainsRune("|;,", line[end-1]) ||
		label.MatchString(quote) || !strings.ContainsFunc(quote, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) {
		return false
	}
	earlier := label.FindAllStringSubmatch(string(line[:start]), -1)
	if len(earlier) != 0 {
		role := strings.ToLower(earlier[len(earlier)-1][1])
		switch role {
		case "band", "performer":
			role = "artist"
		case "concert date":
			role = "date"
		}
		if role != field {
			return false
		}
	}
	return field != "date" || dateLike(quote)
}

func (e *encoder) logits(tokens []int, outputTokens int) []float32 {
	n := len(tokens)
	x := make([]float32, n*width)
	for i, token := range tokens {
		row := x[i*width : (i+1)*width]
		words := e.tokens[token*width : (token+1)*width]
		positions := e.positions[i*width : (i+1)*width]
		for j, word := range words {
			row[j] = (float32(int8(word))*e.scales[token] + e.tokenType[j]) + positions[j]
		}
		e.embeddingsNorm.apply(row)
	}

	query := make([]float32, len(x))
	key := make([]float32, len(x))
	value := make([]float32, len(x))
	attended := make([]float32, len(x))
	projected := make([]float32, len(x))
	expanded := make([]float32, intermediate)
	contracted := make([]float32, width)
	attention := make([]float32, n)
	for layerIdx, layer := range e.layers {
		required := n
		if layerIdx == len(e.layers)-1 {
			// The second layer needs context keys and values, but downstream
			// boundary selection reads only CLS and focal-token queries.
			required = outputTokens
		}
		layer.query.applyBatch(query, x, required)
		layer.key.applyBatch(key, x, n)
		layer.value.applyBatch(value, x, n)
		clear(attended[:required*width])
		attend(attended, attention, query, key, value, n, required)
		layer.project.applyBatch(projected, attended, required)
		for i := range required {
			row := x[i*width : (i+1)*width]
			addTo(row, projected[i*width:(i+1)*width])
			layer.attentionNorm.apply(row)
			layer.expand.apply(expanded, row)
			for j, a := range expanded {
				expanded[j] = a * float32(0.5*(1+math.Erf(float64(a)/math.Sqrt2)))
			}
			layer.contract.apply(contracted, expanded)
			addTo(row, contracted)
			layer.outputNorm.apply(row)
		}
	}

	out := make([]float32, outputTokens*e.head.out)
	e.head.applyBatch(out, x, outputTokens)
	return out
}

func addTo(dst, src []float32) {
	for i, value := range src {
		dst[i] += value
	}
}

func (norm normalization) apply(x []float32) {
	var mean, variance float32
	for _, value := range x {
		mean += value
	}
	mean *= 1.0 / width
	for _, value := range x {
		diff := value - mean
		variance += diff * diff
	}
	inverse := float32(1 / math.Sqrt(float64(variance*(1.0/width)+1e-12)))
	for i, value := range x {
		x[i] = ((value-mean)*inverse)*norm.weight[i] + norm.bias[i]
	}
}

func (d dense) apply(output, input []float32) {
	for j, bias := range d.bias {
		output[j] = dot(input, d.weights[j*d.in:(j+1)*d.in]) + bias
	}
}

func (d dense) applyBatch(output, input []float32, count int) {
	for i := range count {
		d.apply(output[i*d.out:(i+1)*d.out], input[i*d.in:(i+1)*d.in])
	}
}

func attend(output, scores, query, key, value []float32, n, required int) {
	const headWidth = width / 2
	for position := range required {
		for head := range 2 {
			offset := head * headWidth
			q := query[position*width+offset : position*width+offset+headWidth]
			dst := output[position*width+offset : position*width+offset+headWidth]
			maxScore := float32(math.Inf(-1))
			for j := range n {
				k := key[j*width+offset : j*width+offset+headWidth]
				score := dot(q, k) * (1.0 / 8)
				scores[j] = score
				maxScore = max(score, maxScore)
			}
			var total float32
			for j, score := range scores {
				prob := float32(math.Exp(float64(score - maxScore)))
				scores[j] = prob
				total += prob
			}
			for j, weight := range scores {
				weight /= total
				src := value[j*width+offset : j*width+offset+headWidth]
				for l, v := range src {
					dst[l] += v * weight
				}
			}
		}
	}
}
