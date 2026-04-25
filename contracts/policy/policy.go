package policy

import (
	"embed"
	"encoding/json"
	"sync"
)

//go:embed route-classes.json role-policy.json device-profiles.json radio-budget.json security-modes.json
var artifacts embed.FS

type RouteClassArtifact struct {
	Version      string                      `json:"version"`
	RouteClasses map[string]RouteClassPolicy `json:"route_classes"`
}

type RouteClassPolicy struct {
	AllowedBearers     []string `json:"allowed_bearers"`
	ForbiddenBearers   []string `json:"forbidden_bearers"`
	RequiresTargetRole []string `json:"requires_target_role"`
	MaxLoRaBodyBytes   *int     `json:"max_lora_body_bytes"`
	AllowRelay         bool     `json:"allow_relay"`
	AllowRedundant     bool     `json:"allow_redundant"`
	HopLimit           *int     `json:"hop_limit"`
	ServiceLevel       string   `json:"service_level"`
	PendingPolicy      string   `json:"pending_policy"`
}

type RolePolicyArtifact struct {
	Version string                `json:"version"`
	Roles   map[string]RolePolicy `json:"roles"`
}

type RolePolicy struct {
	RequiresAlwaysOn bool `json:"requires_always_on"`
	MayDeepSleep     bool `json:"may_deep_sleep"`
	MayRelay         bool `json:"may_relay"`
	MayWiFiMeshRoute bool `json:"may_wifi_mesh_route"`
	MayLoRaRelay     bool `json:"may_lora_relay"`
}

type DeviceProfileArtifact struct {
	Version  string                         `json:"version"`
	Profiles map[string]DeviceProfilePolicy `json:"profiles"`
}

type DeviceProfilePolicy struct {
	DeviceFamily     string            `json:"device_family"`
	PowerClass       string            `json:"power_class"`
	WakeClass        string            `json:"wake_class"`
	AllowedRoles     []string          `json:"allowed_roles"`
	SupportedBearers []string          `json:"supported_bearers"`
	DefaultRoutes    map[string]string `json:"default_routes"`
	Forbidden        map[string]bool   `json:"forbidden"`
}

type RadioBudgetArtifact struct {
	Version      string                       `json:"version"`
	RegionPolicy string                       `json:"region_policy"`
	RuntimeGate  string                       `json:"runtime_gate"`
	Defaults     RadioBudgetPolicy            `json:"defaults"`
	RouteClasses map[string]RadioBudgetPolicy `json:"route_classes"`
}

type RadioBudgetPolicy struct {
	Profile                string `json:"profile"`
	MaxAirtimeMS           int    `json:"max_airtime_ms"`
	OccupancyWindowSeconds int    `json:"occupancy_window_seconds"`
	MaxWindowAirtimeMS     int    `json:"max_window_airtime_ms"`
}

type SecurityModeArtifact struct {
	Version     string                        `json:"version"`
	DefaultMode string                        `json:"default_mode"`
	Modes       map[string]SecurityModePolicy `json:"modes"`
}

type SecurityModePolicy struct {
	IntendedUse                    string `json:"intended_use"`
	AllowLegacyLoRaJSON            bool   `json:"allow_legacy_lora_json"`
	AllowDeclaredLoRaSizeForAlpha  bool   `json:"allow_declared_lora_size_for_alpha"`
	RequireBinaryOnAirForLoRa      bool   `json:"require_binary_onair_for_lora"`
	RequireStrictHeartbeatSubject  bool   `json:"require_strict_heartbeat_subject"`
	EnforceRadioBudget             bool   `json:"enforce_radio_budget"`
	AllowTestKeys                  bool   `json:"allow_test_keys"`
	AllowRealKeyMaterialWithoutHIL bool   `json:"allow_real_key_material_without_hil"`
}

var (
	routeOnce sync.Once
	routeData RouteClassArtifact
	routeErr  error

	roleOnce sync.Once
	roleData RolePolicyArtifact
	roleErr  error

	profileOnce sync.Once
	profileData DeviceProfileArtifact
	profileErr  error

	radioBudgetOnce sync.Once
	radioBudgetData RadioBudgetArtifact
	radioBudgetErr  error

	securityModeOnce sync.Once
	securityModeData SecurityModeArtifact
	securityModeErr  error
)

func LoadRouteClasses() (RouteClassArtifact, error) {
	routeOnce.Do(func() {
		routeErr = loadJSON("route-classes.json", &routeData)
	})
	return routeData, routeErr
}

func LoadRolePolicy() (RolePolicyArtifact, error) {
	roleOnce.Do(func() {
		roleErr = loadJSON("role-policy.json", &roleData)
	})
	return roleData, roleErr
}

func LoadDeviceProfiles() (DeviceProfileArtifact, error) {
	profileOnce.Do(func() {
		profileErr = loadJSON("device-profiles.json", &profileData)
	})
	return profileData, profileErr
}

func LoadRadioBudget() (RadioBudgetArtifact, error) {
	radioBudgetOnce.Do(func() {
		radioBudgetErr = loadJSON("radio-budget.json", &radioBudgetData)
	})
	return radioBudgetData, radioBudgetErr
}

func LoadSecurityModes() (SecurityModeArtifact, error) {
	securityModeOnce.Do(func() {
		securityModeErr = loadJSON("security-modes.json", &securityModeData)
	})
	return securityModeData, securityModeErr
}

func MustRouteClass(id string) (RouteClassPolicy, bool) {
	artifact, err := LoadRouteClasses()
	if err != nil {
		return RouteClassPolicy{}, false
	}
	policy, ok := artifact.RouteClasses[id]
	return policy, ok
}

func MustRole(id string) (RolePolicy, bool) {
	artifact, err := LoadRolePolicy()
	if err != nil {
		return RolePolicy{}, false
	}
	policy, ok := artifact.Roles[id]
	return policy, ok
}

func loadJSON(path string, out any) error {
	raw, err := artifacts.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
