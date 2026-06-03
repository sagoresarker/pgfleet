package events

import (
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

func TestNewEventValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      NewEvent
		wantErr bool
	}{
		{
			name: "valid",
			in:   NewEvent{Type: "status_change", Message: "running"},
		},
		{
			name:    "missing type",
			in:      NewEvent{Message: "running"},
			wantErr: true,
		},
		{
			name:    "missing message",
			in:      NewEvent{Type: "status_change"},
			wantErr: true,
		},
		{
			name:    "blank type",
			in:      NewEvent{Type: "   ", Message: "running"},
			wantErr: true,
		},
		{
			name:    "blank message",
			in:      NewEvent{Type: "status_change", Message: "  "},
			wantErr: true,
		},
		{
			name: "valid with all fields",
			in: NewEvent{
				InstanceID: "11111111-1111-1111-1111-111111111111",
				ClusterID:  "22222222-2222-2222-2222-222222222222",
				Type:       "health_transition",
				Message:    "healthy -> unhealthy",
				Metadata:   map[string]string{"from": "healthy", "to": "unhealthy"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Validate() = nil, want error")
				}
				if apperr.Kind(err) != apperr.KindInvalid {
					t.Errorf("Validate() kind = %v, want KindInvalid", apperr.Kind(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}
