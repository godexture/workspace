package tag_test

import (
	"fmt"

	"github.com/godexture/godec/media/tag"
)

// Partial dates retain the precision present in the carrier instead of
// inventing missing calendar fields.
func ExampleParseDate() {
	year, _ := tag.ParseDate("1985")
	day, _ := tag.ParseDate("1985-10-26")
	_, yearHasMonth := year.Month()

	fmt.Println(year, yearHasMonth)
	fmt.Println(day)
	// Output:
	// 1985 false
	// 1985-10-26
}

// Declarations exposes the common vocabulary for optional host validation.
func ExampleDeclarations() {
	declarations := tag.Declarations()
	fmt.Println(len(declarations), declarations[0].Key().Name() == tag.Title().ID().String())
	// Output: 17 true
}
