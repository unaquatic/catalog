package data

import (
	"testing"

	"github.com/unaquatic/catalog/gateway"
)

// TestCatalog checks that the embedded catalog reads, of the current
// format, with models and providers, and the same each call.
func TestCatalog(t *testing.T) {
	c, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != gateway.Version || len(c.Models) == 0 || len(c.Providers) == 0 {
		t.Errorf("catalog = version %d, %d models, %d providers", c.Version, len(c.Models), len(c.Providers))
	}
	if again, _ := Catalog(); again != c {
		t.Error("Catalog read the catalog again")
	}
}
