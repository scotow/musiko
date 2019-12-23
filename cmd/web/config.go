package main

import (
	"fmt"
	"github.com/pkg/errors"
	"strings"
)

var (
	errInvalidStation = errors.New("invalid station config (name:id)")
)

type config struct {
	id      string
	Name    string `json:"name"`
	Display string `json:"display"`
}

type configFlags []config

func (c *configFlags) String() string {
	configs := make([]string, len(*c))

	for i, s := range *c {
		configs[i] = fmt.Sprintf("%s:%s", s.Name, s.Display)
	}
	return strings.Join(configs, " ")
}

func (c *configFlags) Set(value string) error {
	parts := strings.SplitN(value, ":", 3)
	if len(parts) != 3 {
		return errInvalidStation
	}
	*c = append(*c, config{parts[0], parts[1], parts[2]})

	return nil
}
