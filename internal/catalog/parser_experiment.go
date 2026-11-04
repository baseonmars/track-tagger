//go:build parser_experiment

package catalog

// ExperimentTitles exposes raw note title candidates only to the experiment build.
func ExperimentTitles(notes []string) map[string]string {
	titles := map[string]string{}
	dates, artists := map[string]bool{}, map[string]bool{}
	probe := Recording{}
	for _, note := range notes {
		parseNotes(&probe, note, titles, dates, artists)
	}
	return titles
}

// ExperimentFileNumber exposes the production filename identifier rules to experiments.
func ExperimentFileNumber(path string) (int, int) { return fileNumber(path) }
