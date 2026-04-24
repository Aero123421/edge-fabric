package fabric

type DeviceProfile struct {
	ID               string
	DeviceFamily     string
	PowerClass       string
	WakeClass        string
	AllowedRoles     []string
	SupportedBearers []string
	DefaultRoutes    map[string]string
	Forbidden        map[string]bool
}

func MotionSensorBatteryProfile() DeviceProfile {
	return DeviceProfile{
		ID:               "motion_sensor_battery_v1",
		DeviceFamily:     "xiao-esp32s3-sx1262",
		PowerClass:       "primary_battery",
		WakeClass:        "sleepy_event",
		AllowedRoles:     []string{"sleepy_leaf"},
		SupportedBearers: []string{"lora", "ble_maintenance"},
		DefaultRoutes: map[string]string{
			"event":     "critical_alert",
			"state":     "sparse_summary",
			"heartbeat": "sleepy_heartbeat",
		},
		Forbidden: map[string]bool{
			"relay":            true,
			"wifi_mesh_router": true,
			"bulk":             true,
		},
	}
}

func LeakSensorSleepyProfile() DeviceProfile {
	profile := MotionSensorBatteryProfile()
	profile.ID = "leak_sensor_sleepy_v1"
	return profile
}

func PoweredServoControllerProfile() DeviceProfile {
	return DeviceProfile{
		ID:               "servo_controller_powered_v1",
		DeviceFamily:     "xiao-esp32s3-sx1262",
		PowerClass:       "mains_powered",
		WakeClass:        "always_on",
		AllowedRoles:     []string{"powered_leaf"},
		SupportedBearers: []string{"wifi"},
		DefaultRoutes: map[string]string{
			"command": "local_control",
			"state":   "normal_state",
			"event":   "control_event",
		},
		Forbidden: map[string]bool{
			"lora_realtime_control": true,
			"deep_sleep":            true,
		},
	}
}
