package core

import "fmt"

type Counter map[string]int

func (c Counter) Add(s string) {
	c[s] += 1
}

func (c Counter) String() string {
	s := ""
	for k, v := range c {
		s += fmt.Sprintf("% 8d | %s\n", v, k)
	}
	return s
}

func NewCounter() Counter {
	return make(Counter)
}
