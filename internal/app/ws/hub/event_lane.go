package hub

type eventLane string

const (
	laneHigh   eventLane = "high"
	laneLatest eventLane = "latest"
	laneNormal eventLane = "normal"
)

func classifyEventLane(eventType string) eventLane {
	switch eventType {
	case "time_sync":
		return laneLatest
	case "user_outbid", "auction_extended", "auction_ended", "auction_cancelled", "order_created", "auction_started", "auction_snapshot":
		return laneHigh
	default:
		return laneNormal
	}
}
