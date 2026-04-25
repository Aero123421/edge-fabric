package jp

import (
	"encoding/json"
	"fmt"
	"math"
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

func AirtimeMSForProfile(path, name string, totalPayloadBytes int) (int, error) {
	profiles, err := LoadProfileFile(path)
	if err != nil {
		return 0, err
	}
	for _, profile := range profiles.Profiles {
		if profile.Name == name {
			return AirtimeMS(profile, totalPayloadBytes)
		}
	}
	return 0, fmt.Errorf("unknown JP-safe profile: %s", name)
}

func AirtimeMS(profile Profile, totalPayloadBytes int) (int, error) {
	if profile.BandwidthKHz <= 0 {
		return 0, fmt.Errorf("profile %s has invalid bandwidth", profile.Name)
	}
	if profile.SpreadingFactor < 6 || profile.SpreadingFactor > 12 {
		return 0, fmt.Errorf("profile %s has invalid spreading factor", profile.Name)
	}
	if totalPayloadBytes < 0 {
		return 0, fmt.Errorf("payload bytes must be non-negative")
	}
	const (
		preambleSymbols = 8.0
		codingRateDenom = 1.0 // CR 4/5, represented as 1 in Semtech airtime formula.
		explicitHeader  = 0.0
		crcEnabled      = 1.0
	)
	sf := float64(profile.SpreadingFactor)
	bwHz := float64(profile.BandwidthKHz) * 1000
	lowDataRateOptimize := 0.0
	if profile.SpreadingFactor >= 11 && profile.BandwidthKHz == 125 {
		lowDataRateOptimize = 1.0
	}
	symbolSeconds := math.Pow(2, sf) / bwHz
	preambleSeconds := (preambleSymbols + 4.25) * symbolSeconds
	payloadNumerator := 8*float64(totalPayloadBytes) - 4*sf + 28 + 16*crcEnabled - 20*explicitHeader
	payloadDenominator := 4 * (sf - 2*lowDataRateOptimize)
	payloadSymbols := 8.0 + math.Max(math.Ceil(payloadNumerator/payloadDenominator)*(codingRateDenom+4), 0)
	airtimeSeconds := preambleSeconds + payloadSymbols*symbolSeconds
	return int(math.Ceil(airtimeSeconds * 1000)), nil
}
