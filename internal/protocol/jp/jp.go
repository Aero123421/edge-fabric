package jp

import (
	"encoding/json"
	"fmt"
	"os"
)

const (
	DirectUplinkOverheadBytes  = 14
	RelayedUplinkOverheadBytes = 18
)

type Profile struct {
	Name            string `json:"name"`
	BandwidthKHz    int    `json:"bandwidth_khz"`
	SpreadingFactor int    `json:"spreading_factor"`
	TotalPayloadCap int    `json:"total_payload_cap"`
}

type ProfileFile struct {
	RegionPolicy   string    `json:"region_policy"`
	CADOnlyAllowed bool      `json:"cad_only_allowed"`
	Profiles       []Profile `json:"profiles"`
}

func LoadProfileFile(path string) (*ProfileFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var profiles ProfileFile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, err
	}
	return &profiles, nil
}

func BodyCapForProfile(path, name string, relayed bool) (int, error) {
	profiles, err := LoadProfileFile(path)
	if err != nil {
		return 0, err
	}
	for _, profile := range profiles.Profiles {
		if profile.Name != name {
			continue
		}
		overhead := DirectUplinkOverheadBytes
		if relayed {
			overhead = RelayedUplinkOverheadBytes
		}
		return profile.TotalPayloadCap - overhead, nil
	}
	return 0, fmt.Errorf("unknown JP-safe profile: %s", name)
}
