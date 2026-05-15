package siterouter

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Aero123421/edge-fabric/pkg/contracts"
)

type RouteReplanResult struct {
	Scanned      int64 `json:"scanned"`
	Updated      int64 `json:"updated"`
	Skipped      int64 `json:"skipped"`
	ReadyToSend  int64 `json:"ready_to_send"`
	RoutePending int64 `json:"route_pending"`
	RouteBlocked int64 `json:"route_blocked"`
	Failed       int64 `json:"failed"`
}

type ReplanWorker struct {
	Router *Router
	Limit  int
}

func (w ReplanWorker) RunOnce(ctx context.Context) (*RouteReplanResult, error) {
	if w.Router == nil {
		return nil, errors.New("router is required")
	}
	return w.Router.ReplanQueuedRoutes(ctx, w.Limit)
}

func (r *Router) ReplanQueuedRoutes(ctx context.Context, limit int) (*RouteReplanResult, error) {
	if limit <= 0 {
		limit = 32
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, envelope_json
		FROM outbox_queue
		WHERE status = 'queued'
		  AND route_status IN ('route_pending', 'route_blocked')
		ORDER BY updated_at, created_at
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type replanCandidate struct {
		queueID int64
		raw     string
	}
	var candidates []replanCandidate
	for rows.Next() {
		var candidate replanCandidate
		if err := rows.Scan(&candidate.queueID, &candidate.raw); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := &RouteReplanResult{}
	for _, candidate := range candidates {
		result.Scanned++
		var envelope contracts.Envelope
		if err := json.Unmarshal([]byte(candidate.raw), &envelope); err != nil {
			result.Failed++
			if _, _, updateErr := r.updateQueueRoutePlan(ctx, candidate.queueID, replanErrorRoutePlan("invalid_envelope_json", err)); updateErr != nil {
				return result, updateErr
			}
			continue
		}
		plan, err := r.PlanOutboundRoute(ctx, &envelope)
		if err != nil {
			result.Failed++
			plan = replanErrorRoutePlan("route_validation_error", err)
			plan.RouteClass = deliveryRouteClass(envelope.Delivery)
		}
		status, updated, err := r.updateQueueRoutePlan(ctx, candidate.queueID, plan)
		if err != nil {
			return result, err
		}
		if !updated {
			result.Skipped++
			continue
		}
		result.Updated++
		switch status {
		case "ready_to_send":
			result.ReadyToSend++
		case "route_blocked":
			result.RouteBlocked++
		default:
			result.RoutePending++
		}
	}
	return result, nil
}

func (r *Router) updateQueueRoutePlan(ctx context.Context, queueID int64, plan *RoutePlan) (string, bool, error) {
	routePlanJSON, err := json.Marshal(plan)
	if err != nil {
		return "", false, err
	}
	routeStatus := routeStatusForPlan(plan)
	selectedBearer := ""
	selectedGatewayID := ""
	routeReason := ""
	payloadFit := false
	if plan != nil {
		selectedBearer = plan.Bearer
		selectedGatewayID = plan.GatewayID
		routeReason = plan.Reason
		payloadFit = plan.PayloadFit
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE outbox_queue
		SET route_status = ?,
			selected_bearer = NULLIF(?, ''),
			selected_gateway_id = NULLIF(?, ''),
			route_reason = NULLIF(?, ''),
			payload_fit = ?,
			route_plan_json = ?,
			updated_at = ?
		WHERE id = ?
		  AND status = 'queued'
		  AND route_status IN ('route_pending', 'route_blocked')
	`, routeStatus, selectedBearer, selectedGatewayID, routeReason, boolToInt(payloadFit), string(routePlanJSON), time.Now().UTC().Format(time.RFC3339Nano), queueID)
	if err != nil {
		return "", false, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return "", false, nil
	}
	return routeStatus, true, nil
}

func replanErrorRoutePlan(reason string, err error) *RoutePlan {
	detail := map[string]any{}
	if err != nil {
		detail["error"] = err.Error()
	}
	return &RoutePlan{
		Bearer:     "blocked",
		PayloadFit: false,
		Reason:     reason,
		Detail:     detail,
	}
}
