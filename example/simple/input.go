//go:generate go run github.com/rahimlis/go-converter/cmd/goverter github.com/rahimlis/go-converter/example/simple
package simple

// goverter:converter
type Converter interface {
	Convert(source []Input) []Output
}

type Input struct {
	Name string
	Age  int
}

type Output struct {
	Name string
	Age  int
}
