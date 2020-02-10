package bar

import "strconv"

type Bar struct {
	Alpha
}

type Baz struct{}

type Alpha struct{}

func (b Bar) Const() int {
	b.AnotherMethodCalledFromUsedOne()
	return 42
}

func (b Bar) SomeUnusedMethod(baz Baz) float64 {
	return 42
}

func (b Bar) AnotherMethodCalledFromUsedOne() {
}

func (a Alpha) EmbeddedMethod(i int) string {
	return strconv.Itoa(i)
}

func (a Alpha) AnotherAlphaMethod() {}
