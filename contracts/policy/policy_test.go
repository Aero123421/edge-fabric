package policy

import "testing"

func TestEmbeddedPolicyArtifactsLoad(t *testing.T) {
	routes, err := LoadRouteClasses()
	if err != nil {
		t.Fatal(err)
	}
	roles, err := LoadRolePolicy()
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := LoadDeviceProfiles()
	if err != nil {
		t.Fatal(err)
	}
	budget, err := LoadRadioBudget()
	if err != nil {
		t.Fatal(err)
	}
	security, err := LoadSecurityModes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes.RouteClasses) == 0 || len(roles.Roles) == 0 || len(profiles.Profiles) == 0 {
		t.Fatal("policy artifacts must not be empty")
	}
	if _, ok := security.Modes[security.DefaultMode]; !ok {
		t.Fatalf("default security mode %q is not declared", security.DefaultMode)
	}
	for routeClass := range budget.RouteClasses {
		if _, ok := routes.RouteClasses[routeClass]; !ok {
			t.Fatalf("radio budget references unknown route class %q", routeClass)
		}
	}
}

func TestRoutePoliciesReferenceKnownRolesAndDefaults(t *testing.T) {
	routes, err := LoadRouteClasses()
	if err != nil {
		t.Fatal(err)
	}
	roles, err := LoadRolePolicy()
	if err != nil {
		t.Fatal(err)
	}
	for routeClass, routePolicy := range routes.RouteClasses {
		if routePolicy.ServiceLevel == "" {
			t.Fatalf("%s must declare service_level", routeClass)
		}
		for _, role := range routePolicy.RequiresTargetRole {
			if _, ok := roles.Roles[role]; !ok {
				t.Fatalf("%s requires unknown role %q", routeClass, role)
			}
		}
		if routePolicy.HopLimit != nil && *routePolicy.HopLimit < 0 {
			t.Fatalf("%s has negative hop_limit", routeClass)
		}
		if routePolicy.MaxLoRaBodyBytes != nil && *routePolicy.MaxLoRaBodyBytes <= 0 {
			t.Fatalf("%s has invalid max_lora_body_bytes=%d", routeClass, *routePolicy.MaxLoRaBodyBytes)
		}
	}
}

func TestDeviceProfilesReferenceKnownRouteClassesAndRoles(t *testing.T) {
	routes, err := LoadRouteClasses()
	if err != nil {
		t.Fatal(err)
	}
	roles, err := LoadRolePolicy()
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := LoadDeviceProfiles()
	if err != nil {
		t.Fatal(err)
	}
	for profileID, profile := range profiles.Profiles {
		if profile.DeviceFamily == "" {
			t.Fatalf("%s must declare device_family", profileID)
		}
		for _, role := range profile.AllowedRoles {
			if _, ok := roles.Roles[role]; !ok {
				t.Fatalf("%s allows unknown role %q", profileID, role)
			}
		}
		for purpose, routeClass := range profile.DefaultRoutes {
			if _, ok := routes.RouteClasses[routeClass]; !ok {
				t.Fatalf("%s default route %s references unknown route class %q", profileID, purpose, routeClass)
			}
		}
	}
}

func TestSecurityModesTightenTowardProduction(t *testing.T) {
	security, err := LoadSecurityModes()
	if err != nil {
		t.Fatal(err)
	}
	dev := security.Modes["dev"]
	field := security.Modes["field-alpha"]
	production := security.Modes["production"]
	if !dev.AllowDeclaredLoRaSizeForAlpha || !field.AllowDeclaredLoRaSizeForAlpha {
		t.Fatal("dev and field-alpha should keep explicit declared-size alpha compatibility")
	}
	if production.AllowDeclaredLoRaSizeForAlpha || production.AllowTestKeys {
		t.Fatal("production must disable declared-size alpha compatibility and test keys")
	}
	if !field.RequireStrictHeartbeatSubject || !production.RequireStrictHeartbeatSubject {
		t.Fatal("field-alpha and production must require strict heartbeat subject metadata")
	}
	if !field.EnforceRadioBudget || !production.EnforceRadioBudget {
		t.Fatal("field-alpha and production must enforce RadioBudget")
	}
}
