package fabric

import "errors"

var (
	ErrPayloadTooLarge         = errors.New("payload too large")
	ErrRouteUnavailable        = errors.New("route unavailable")
	ErrRoutePending            = errors.New("route pending")
	ErrLeaseMissing            = errors.New("lease missing")
	ErrManifestMissing         = errors.New("manifest missing")
	ErrRoleDenied              = errors.New("role denied")
	ErrBearerForbidden         = errors.New("bearer forbidden")
	ErrCommandNotRepresentable = errors.New("command not representable")
	ErrTokenExhausted          = errors.New("command token exhausted")
	ErrSecurityRequired        = errors.New("security required")
	ErrGatewayUnavailable      = errors.New("gateway unavailable")
	ErrPolicyDrift             = errors.New("policy artifact drift")
)
