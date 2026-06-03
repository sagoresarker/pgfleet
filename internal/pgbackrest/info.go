package pgbackrest

import (
	"encoding/json"
	"fmt"
	"time"
)

// Stanza is a parsed pgBackRest stanza from `info --output=json`.
type Stanza struct {
	Name          string
	StatusCode    int
	StatusMessage string
	Backups       []BackupInfo
}

// Healthy reports whether the stanza status code indicates a good state.
func (s Stanza) Healthy() bool { return s.StatusCode == 0 }

// Backup is a parsed backup entry. Sizes in the repository (RepoSize/RepoDelta)
// reflect actual stored bytes; Size/Delta are logical database sizes.
type BackupInfo struct {
	Label      string
	Type       string // full | diff | incr
	StartTime  time.Time
	StopTime   time.Time
	Size       int64
	Delta      int64
	RepoSize   int64
	RepoDelta  int64
	WALStart   string
	WALStop    string
	References []string
	Error      bool
}

// --- raw JSON shapes ---

type rawStanza struct {
	Name   string `json:"name"`
	Status struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
	Backup []rawBackup `json:"backup"`
}

type rawBackup struct {
	Label     string `json:"label"`
	Type      string `json:"type"`
	Timestamp struct {
		Start int64 `json:"start"`
		Stop  int64 `json:"stop"`
	} `json:"timestamp"`
	Info struct {
		Size       int64 `json:"size"`
		Delta      int64 `json:"delta"`
		Repository struct {
			Size  int64 `json:"size"`
			Delta int64 `json:"delta"`
		} `json:"repository"`
	} `json:"info"`
	Archive struct {
		Start string `json:"start"`
		Stop  string `json:"stop"`
	} `json:"archive"`
	Reference []string `json:"reference"`
	Error     bool     `json:"error"`
}

// epochToTime converts a pgBackRest epoch-seconds timestamp to UTC. A missing
// timestamp decodes to 0; pgBackRest never emits a real 1970 timestamp, so a
// non-positive value means "absent" and maps to the zero time.Time rather than
// 1970 (which would corrupt catalog ordering and retention).
func epochToTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// ParseInfo parses the output of `pgbackrest info --output=json`.
func ParseInfo(data []byte) ([]Stanza, error) {
	var raw []rawStanza
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("pgbackrest: parse info json: %w", err)
	}

	stanzas := make([]Stanza, 0, len(raw))
	for _, rs := range raw {
		s := Stanza{
			Name:          rs.Name,
			StatusCode:    rs.Status.Code,
			StatusMessage: rs.Status.Message,
		}
		for _, rb := range rs.Backup {
			s.Backups = append(s.Backups, BackupInfo{
				Label:      rb.Label,
				Type:       rb.Type,
				StartTime:  epochToTime(rb.Timestamp.Start),
				StopTime:   epochToTime(rb.Timestamp.Stop),
				Size:       rb.Info.Size,
				Delta:      rb.Info.Delta,
				RepoSize:   rb.Info.Repository.Size,
				RepoDelta:  rb.Info.Repository.Delta,
				WALStart:   rb.Archive.Start,
				WALStop:    rb.Archive.Stop,
				References: rb.Reference,
				Error:      rb.Error,
			})
		}
		stanzas = append(stanzas, s)
	}
	return stanzas, nil
}
