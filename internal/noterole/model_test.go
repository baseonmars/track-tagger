package noterole

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func fixture() Artifact {
	a := Artifact{SchemaVersion: 1, FeatureVersion: FeatureVersion, ModelID: "synthetic-test", TrainingSHA256: strings.Repeat("a", 64), Vocabulary: []string{"w:1:artist"}}
	for _, r := range Roles {
		a.Heads = append(a.Heads, Head{Role: r, Weights: []float64{-2}, Threshold: .6, Margin: .05})
	}
	a.Heads[0].Weights[0] = 2
	return a
}
func model(t *testing.T, a Artifact) *Model {
	t.Helper()
	b, e := json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	m, e := Load(bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func TestFeaturesUnicodeAndLines(t *testing.T) {
	empty, err := Lines("")
	if err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
	lines, e := Lines("\ufeffÉlan\r\n\tArtist  名\r\n\r\n")
	if e != nil || !reflect.DeepEqual(lines, []string{"Élan", "\tArtist  名", ""}) {
		t.Fatalf("%q %v", lines, e)
	}
	f, e := Features(lines[1], lines[0], "")
	if e != nil {
		t.Fatal(e)
	}
	for _, k := range []string{"w:1:artist", "w:1:名", "w:2:artist 名", "p:1:élan", "c:2:ar", "boundary:next-empty"} {
		if f[k] != 1 {
			t.Errorf("%s=%v", k, f[k])
		}
	}
}
func TestIndependentRolesAndAbstention(t *testing.T) {
	a := fixture()
	m := model(t, a)
	s, e := m.Suggest("artist", "", "")
	if e != nil || !reflect.DeepEqual(s.Roles, []string{"artist"}) {
		t.Fatal(s, e)
	}
	if math.Abs(s.Scores["artist"]-1/(1+math.Exp(-2))) > 1e-15 {
		t.Fatal(s)
	}
	s, _ = m.Suggest("unknown", "", "")
	if s.Abstention != "zero_or_oov_source_features" || len(s.Roles) != 0 {
		t.Fatal(s)
	}
	a.Heads[4].Weights[0] = 2
	s, _ = model(t, a).Suggest("artist", "", "")
	if !reflect.DeepEqual(s.Roles, []string{"artist", "title"}) {
		t.Fatal(s)
	}
}
func TestRejectArtifacts(t *testing.T) {
	for _, change := range []func(*Artifact){func(a *Artifact) { a.FeatureVersion = "unknown" }, func(a *Artifact) { a.Heads[0].Weights = nil }, func(a *Artifact) { a.Vocabulary = append(a.Vocabulary, "w:1:artist") }, func(a *Artifact) { a.Heads[0].Intercept = math.Copysign(0, -1) }, func(a *Artifact) { a.Heads[0].Threshold = 1 }} {
		a := fixture()
		change(&a)
		b, _ := json.Marshal(a)
		if _, e := Load(bytes.NewReader(b)); e == nil {
			t.Fatalf("accepted %s", b)
		}
	}
	if _, e := Load(strings.NewReader(strings.Repeat(" ", MaxModelBytes+1))); e == nil {
		t.Fatal("oversized accepted")
	}
	if _, e := Features(strings.Repeat("x", MaxLineRunes+1), "", ""); e == nil {
		t.Fatal("oversized line accepted")
	}
}

func TestFiniteOverflowRejectsWholeSuggestion(t *testing.T) {
	a := fixture()
	a.Heads[0].Weights[0] = 1e308
	if _, err := model(t, a).Suggest("artist artist artist", "", ""); err == nil {
		t.Fatal("overflow accepted")
	}
}
