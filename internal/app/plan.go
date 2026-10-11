package app

import (
	"encoding/json"
	"errors"
	"os"
)

func SavePlan(path string, p Plan, source, destination string) error {
	real, err := canonical(path)
	if err != nil {
		return err
	}
	if inside(source, real) || (destination != "" && inside(destination, real)) {
		return errors.New("save plans outside source and destination trees")
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(real, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(b); err != nil {
		return err
	}
	return f.Sync()
}
func NewHistory(state string, mode Mode) (*Engine, error) {
	p, err := canonical(state)
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(p + string(os.PathSeparator) + "history.sqlite"); err != nil {
		return nil, err
	}
	e := &Engine{Options: Options{State: p, Mode: mode}}
	return e, e.OpenStorage()
}
