//go:generate go run github.com/rahimlis/go-converter/cmd/goverter github.com/rahimlis/go-converter/example/errors
package errors

import (
	"fmt"
	"strings"
)

// This example illustrates, that goverter automatically handles errors in sub converters

// goverter:converter
// goverter:extend ToDBPerson ToAPIPerson
type Converter interface {
	ToAPIApartment(source DBApartment) APIApartment
	ToDBApartment(source APIApartment) (DBApartment, error)
}

func ToAPIPerson(value DBPerson) APIPerson {
	return APIPerson{
		ID:       value.ID,
		FullName: fmt.Sprintf("%s %s", value.FirstName, value.LastName),
	}
}

func ToDBPerson(value APIPerson) (DBPerson, error) {
	names := strings.Fields(value.FullName)
	if len(names) != 2 {
		return DBPerson{}, fmt.Errorf("could not convert")
	}
	person := DBPerson{
		ID:        value.ID,
		FirstName: names[0],
		LastName:  names[1],
	}
	return person, nil
}

type DBApartment struct {
	Position uint
	Owner    DBPerson
}

type DBPerson struct {
	ID        int
	FirstName string
	LastName  string
}

type APIApartment struct {
	Position uint
	Owner    APIPerson
}

type APIPerson struct {
	ID       int
	FullName string
}
