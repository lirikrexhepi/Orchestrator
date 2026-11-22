package antigravity

import (
	"context"
	"strings"
	"testing"
)

func TestCatalogWithRealProfile(t *testing.T) {
	InvalidateAccountModels("")
	models, err := FetchAccountModels(context.Background(), "agy", "lirikrexhepiii-neon", "keyring", "")
	if err != nil {
		t.Fatalf("FetchAccountModels error: %v", err)
	}
	t.Logf("Fetched %d models:", len(models))
	for i, m := range models {
		choices := ""
		if len(m.Options) > 0 {
			for _, opt := range m.Options {
				if opt.ID == "effort" {
					for _, c := range opt.Choices {
						choices += c.ID + " "
					}
				}
			}
		}
		t.Logf("[%d] ID=%q DisplayName=%q Efforts=[%s]", i, m.ID, m.DisplayName, strings.TrimSpace(choices))
	}
}
