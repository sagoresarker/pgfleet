package cluster

import (
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

func TestNewClusterValidate(t *testing.T) {
	if err := (NewCluster{Name: "orders"}).Validate(); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
	for _, bad := range []string{"", "Orders", "1cluster", "has_underscore", "-leading"} {
		if err := (NewCluster{Name: bad}).Validate(); apperr.Kind(err) != apperr.KindInvalid {
			t.Errorf("name %q should be invalid", bad)
		}
	}
}
