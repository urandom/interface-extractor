package bar

type Bar struct{}

type Baz struct{}

func (b Bar) Const() int {
	b.AnotherMethodCalledFromUsedOne()
	return 42
}

func (b Bar) SomeUnusedMethod(baz Baz) float64 {
	return 42
}

func (b Bar) AnotherMethodCalledFromUsedOne() {
}
