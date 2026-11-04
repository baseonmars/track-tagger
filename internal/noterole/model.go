package noterole

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
)

const MaxModelBytes = 10 << 20
const SchemaVersion = 1

var Roles = []string{"artist", "date", "venue", "city", "title", "heading", "event", "continuation", "technical_provenance", "other", "blank"}

type Head struct {
	Role      string    `json:"role"`
	Weights   []float64 `json:"weights"`
	Intercept float64   `json:"intercept"`
	Threshold float64   `json:"threshold"`
	Margin    float64   `json:"margin"`
}

// Artifact contains only passive numeric parameters. Loading never executes code.
type Artifact struct {
	SchemaVersion  int      `json:"schema_version"`
	FeatureVersion string   `json:"feature_version"`
	ModelID        string   `json:"model_id"`
	TrainingSHA256 string   `json:"training_sha256"`
	Vocabulary     []string `json:"vocabulary"`
	Heads          []Head   `json:"heads"`
}
type Model struct {
	artifact Artifact
	indices  map[string]int
}
type Suggestion struct {
	ModelID       string             `json:"model_id"`
	Scores        map[string]float64 `json:"scores"`
	Roles         []string           `json:"roles"`
	Abstention    string             `json:"abstention,omitempty"`
	KnownFeatures int                `json:"known_features"`
	TotalFeatures int                `json:"total_features"`
}

func numberOK(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && !(v == 0 && math.Signbit(v))
}
func Load(r io.Reader) (*Model, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxModelBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxModelBytes {
		return nil, fmt.Errorf("model exceeds byte bound")
	}
	var a Artifact
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&a); err != nil {
		return nil, err
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("trailing model data")
	}
	if a.SchemaVersion != SchemaVersion || a.FeatureVersion != FeatureVersion || a.ModelID == "" || len(a.ModelID) > 128 || len(a.TrainingSHA256) != 64 {
		return nil, fmt.Errorf("unsupported model identity")
	}
	if _, err := hex.DecodeString(a.TrainingSHA256); err != nil {
		return nil, fmt.Errorf("invalid training digest")
	}
	if len(a.Vocabulary) == 0 || len(a.Vocabulary) > 40000 || len(a.Heads) != len(Roles) {
		return nil, fmt.Errorf("invalid model dimensions")
	}
	m := &Model{artifact: a, indices: make(map[string]int, len(a.Vocabulary))}
	for i, v := range a.Vocabulary {
		if v == "" || len(v) > 4096 || (i > 0 && a.Vocabulary[i-1] >= v) {
			return nil, fmt.Errorf("vocabulary must be bounded sorted unique")
		}
		m.indices[v] = i
	}
	for i, h := range a.Heads {
		if h.Role != Roles[i] || len(h.Weights) != len(a.Vocabulary) || !numberOK(h.Intercept) || !numberOK(h.Threshold) || !numberOK(h.Margin) || h.Threshold <= 0 || h.Threshold >= 1 || h.Margin < 0 || h.Margin >= .5 {
			return nil, fmt.Errorf("invalid head")
		}
		for _, w := range h.Weights {
			if !numberOK(w) {
				return nil, fmt.Errorf("invalid coefficient")
			}
		}
	}
	return m, nil
}

// Suggest classifies roles only. It cannot authorize metadata or readiness.
func (m *Model) Suggest(line, previous, next string) (Suggestion, error) {
	f, err := Features(line, previous, next)
	if err != nil {
		return Suggestion{}, err
	}
	out := Suggestion{ModelID: m.artifact.ModelID, Scores: map[string]float64{}, Roles: []string{}, TotalFeatures: len(f)}
	keys := make([]string, 0, len(f))
	for k := range f {
		if _, ok := m.indices[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out.KnownFeatures = len(keys)
	// Boundary markers alone cannot establish a role for an OOV source line.
	sourceKnown := false
	for _, k := range keys {
		if len(k) > 2 && (k[:2] == "w:" || k[:2] == "c:") {
			sourceKnown = true
		}
	}
	if !sourceKnown {
		out.Abstention = "zero_or_oov_source_features"
		return out, nil
	}
	uncertain := false
	for _, h := range m.artifact.Heads {
		z := h.Intercept
		for _, k := range keys {
			z += f[k] * h.Weights[m.indices[k]]
		}
		if math.IsNaN(z) || math.IsInf(z, 0) {
			return Suggestion{}, fmt.Errorf("nonfinite model score")
		}
		p := 1 / (1 + math.Exp(-z))
		out.Scores[h.Role] = p
		if math.Abs(p-h.Threshold) < h.Margin {
			uncertain = true
		}
		if p >= h.Threshold {
			out.Roles = append(out.Roles, h.Role)
		}
	}
	if len(out.Roles) == 0 {
		out.Abstention = "no_role_above_threshold"
	} else if uncertain {
		out.Abstention = "threshold_margin"
	} else if len(out.Roles) > 1 {
		for _, r := range out.Roles {
			if r == "other" || r == "blank" {
				out.Abstention = "incompatible_roles"
			}
		}
	}
	if out.Abstention != "" {
		out.Roles = []string{}
	}
	return out, nil
}
